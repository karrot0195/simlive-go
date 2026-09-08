package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

var logger = utils.GetLogger("PLANNER")

func PrepareEnv(ctx context.Context, db *gorm.DB, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if cleanUp {
		logger.Info("[Planner] startup: ENABLE_CLEAN_UP=true, flushing all Redis keys + removing TMP_DIR")
		if rdb != nil {
			rdb.FlushAll(ctx)
		}
		tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
		os.RemoveAll(tmpDir)
		if db != nil {
			db.Model(&models.PlayoutChannel{}).
				Where("is_active = ?", true).
				Update("status", "PREPARE")
		}
		return
	}
	logger.Info("[Planner] startup: preserving all Redis keys + TMP_DIR cache")
}

func plannerInterval() time.Duration {
	d, err := time.ParseDuration(utils.GetEnv("PLANNER_INTERVAL", "5m"))
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

func plannerWorkerCount() int {
	n := utils.GetEnvInt("PLANNER_WORKERS", 4)
	if n < 1 {
		return 1
	}
	return n
}

func logMetrics(ctx context.Context, p *planner.Planner, channels []models.PlayoutChannel) {
	for _, ch := range channels {
		size := p.TimelineSize(ctx, ch.ID)
		cursor := p.LastProcessedCursor(ctx, ch.ID)
		cursorStr := "none"
		if cursor > 0 {
			cursorStr = time.Unix(cursor, 0).UTC().Format("2006-01-02T15:04:05Z")
		}
		logger.Info("[METRIC] channel=%s name=%q zset_size=%d cursor=%s live_segment=%d download_ahead=%d",
			ch.ID, ch.Name, size, cursorStr, cursor, 5)
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		logger.Warn("Failed to load .env file: %v", err)
	}

	settings := utils.GetSettings()

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}

	if err := db.AutoMigrate(&models.PlayoutChannel{}); err != nil {
		log.Fatalf("Failed to migrate DB: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	espxClient := api.NewESPXClient(settings.EspxURL)
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)

	mainCtx := context.Background()

	PrepareEnv(mainCtx, db, rdb)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		interval := plannerInterval()
		workers := plannerWorkerCount()
		const pollInterval = 1 * time.Second
		logger.Info(fmt.Sprintf("[Planner] cron started, interval=%s workers=%d poll=%s", interval, workers, pollInterval))

		var inFlightMu sync.Mutex
		inFlight := make(map[string]struct{})

		var cancelMu sync.Mutex
		cancelMap := make(map[string]context.CancelFunc)

		process := func(chCtx context.Context, ch models.PlayoutChannel) {
			loc := utils.GetLocFor(ch.ContentTimezone)
			nowInCh := time.Now().In(loc)
			today := utils.StartOfDay(nowInCh)
			endOfDay := utils.EndOfDay(nowInCh)
			tomorrow := utils.StartOfDay(nowInCh.Add(3 * time.Hour))

			isDiffDate := tomorrow.After(endOfDay)

			logger.Info(fmt.Sprintf("[Planner] Processing active channel: %s (%s), today=%s tomorrow=%s",
				ch.Name, ch.ID, today.Format(configs.DATE_FORMAT_YYYY_MM_DD), tomorrow.Format(configs.DATE_FORMAT_YYYY_MM_DD)))

			// 1. Check if today has data (skips if present, plans if missing)
			prepareDateWithRetry(chCtx, plannerSvc, ch, today)
			if chCtx.Err() != nil {
				logger.Info(fmt.Sprintf("[Planner] channel=%s cancelled during today prepare", ch.ID))
				plannerSvc.CleanChannelData(mainCtx, ch.ID)
				return
			}

			if isDiffDate {
				logger.Info(fmt.Sprintf("process for tomorrow %s", tomorrow.Format(configs.DATE_FORMAT_YYYY_MM_DD)))
				prepareDateWithRetry(chCtx, plannerSvc, ch, tomorrow)
				if chCtx.Err() != nil {
					logger.Info(fmt.Sprintf("[Planner] channel=%s cancelled during tomorrow prepare", ch.ID))
					plannerSvc.CleanChannelData(mainCtx, ch.ID)
					return
				}
			}

			if err := plannerSvc.ProcessWindow(chCtx, ch); err != nil {
				logger.Error("[Planner] ProcessWindow(%s) error: %v", ch.ID, err)
			}

			logger.Info("[PLANER] processed")

			// 3. Clean up expired data AFTER tick processing completes
			plannerSvc.CleanupOldSegments(chCtx, ch.ID)
		}

		// Watcher thread to cancel and wipe Redis data instantly on is_active = false
		go func() {
			pollTicker := time.NewTicker(pollInterval)
			defer pollTicker.Stop()

			for range pollTicker.C {
				var inactiveIDs []string
				db.Model(&models.PlayoutChannel{}).
					Where("is_active = ?", false).
					Pluck("id", &inactiveIDs)

				cancelMu.Lock()
				for _, chID := range inactiveIDs {
					if cancelFunc, exists := cancelMap[chID]; exists {
						logger.Info(fmt.Sprintf("[Planner] Immediate DB deactivation! channel=%s, cancelling & cleaning up Redis...", chID))
						cancelFunc()
						delete(cancelMap, chID)
						plannerSvc.CleanChannelData(mainCtx, chID)
					}
				}
				cancelMu.Unlock()
			}
		}()

		lastActive := map[string]struct{}{}
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()

		channelsEqual := func(a, b map[string]struct{}) bool {
			if len(a) != len(b) {
				return false
			}
			for k := range a {
				if _, ok := b[k]; !ok {
					return false
				}
			}
			return true
		}

		runTick := func(activeChannels []models.PlayoutChannel, activeSet map[string]struct{}) {
			lastActive = activeSet
			if len(activeChannels) == 0 {
				return
			}
			logger.Info(fmt.Sprintf("[Planner] tick: %d active channel(s)", len(activeChannels)))
			logMetrics(mainCtx, plannerSvc, activeChannels)

			runPlannerTick(mainCtx, activeChannels, workers, &inFlightMu, inFlight, &cancelMu, cancelMap, process)
		}

		nextInterval := time.Now().Add(interval)
		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ? AND status != ?", true, "INACTIVE").Find(&activeChannels)

			activeSet := make(map[string]struct{}, len(activeChannels))
			for _, ch := range activeChannels {
				activeSet[ch.ID] = struct{}{}
			}

			intervalDue := time.Now().After(nextInterval)
			changed := !channelsEqual(activeSet, lastActive)

			if changed {
				logger.Info("[Planner] active set change detected")
				runTick(activeChannels, activeSet)
			} else if intervalDue {
				runTick(activeChannels, activeSet)
				nextInterval = time.Now().Add(interval)
			} else {
				lastActive = activeSet
			}

			<-ticker.C
		}
	}()

	wg.Wait()
}

func runPlannerTick(
	parentCtx context.Context,
	activeChannels []models.PlayoutChannel,
	workers int,
	inFlightMu *sync.Mutex,
	inFlight map[string]struct{},
	cancelMu *sync.Mutex,
	cancelMap map[string]context.CancelFunc,
	process func(ctx context.Context, ch models.PlayoutChannel),
) {
	sem := make(chan struct{}, workers)
	var tickWG sync.WaitGroup
	for _, ch := range activeChannels {
		tickWG.Add(1)
		sem <- struct{}{}
		go func(ch models.PlayoutChannel) {
			defer tickWG.Done()
			defer func() { <-sem }()

			inFlightMu.Lock()
			if _, busy := inFlight[ch.ID]; busy {
				inFlightMu.Unlock()
				logger.Info(fmt.Sprintf("[Planner] channel=%s still in flight, skipping", ch.ID))
				return
			}
			inFlight[ch.ID] = struct{}{}
			inFlightMu.Unlock()

			defer func() {
				inFlightMu.Lock()
				delete(inFlight, ch.ID)
				inFlightMu.Unlock()
			}()

			chCtx, cancel := context.WithCancel(parentCtx)
			cancelMu.Lock()
			cancelMap[ch.ID] = cancel
			cancelMu.Unlock()

			defer func() {
				cancelMu.Lock()
				delete(cancelMap, ch.ID)
				cancelMu.Unlock()
				cancel()
			}()

			process(chCtx, ch)
		}(ch)
	}
	tickWG.Wait()
}

func prepareDateWithRetry(ctx context.Context, p *planner.Planner, ch models.PlayoutChannel, date time.Time) {
	if err := ctx.Err(); err != nil {
		return
	}

	loc := utils.GetLocFor(ch.ContentTimezone)
	day := date.In(loc)
	dayStart := utils.StartOfDay(day)
	dayEnd := utils.EndOfDay(day)

	has, err := p.HasDateInTimeline(ctx, ch.ID, dayStart, dayEnd)
	if err != nil {
		logger.Error(fmt.Sprintf("[Planner] HasDateInTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), err))
	} else if has {
		logger.Info("[Planner] channel=%s date=%s already prepared, skipping", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
		p.ResetFailureCounterForChannel(ctx, ch.ID)
		p.MarkChannelLiveForChannel(ctx, ch.ID)
		return
	}

	prepErr := p.ProcessEPGToTimelineWithRetry(ctx, ch, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
	if prepErr != nil && ctx.Err() == nil {
		logger.Info(fmt.Sprintf("[Planner] ProcessEPGToTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), prepErr))
	}

	if ctx.Err() == nil {
		p.HandlePrepareResultForChannel(ctx, ch.ID, prepErr)
	}
}
