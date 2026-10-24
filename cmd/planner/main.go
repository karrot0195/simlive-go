package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/api"
	"simlive/internal/configs"
	customerrors "simlive/internal/custom-errors"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const DIFF_TIME_AHEAD = 3

var logger = utils.GetLogger("PLANNER")

func main() {
	if err := godotenv.Load(); err != nil {
		logger.VWarning(fmt.Sprintf("Failed to load .env file: %v", err))
	}

	settings := utils.GetSettings()

	DBDNS := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(DBDNS), &gorm.Config{})
	if err != nil {
		logger.VFatalf("Failed to connect DB: %v", err)
	}

	if err := db.AutoMigrate(&models.PlayoutChannel{}); err != nil {
		logger.VFatalf("Failed to migrate DB: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	espxClient := api.NewESPXClient(settings.EspxURL)
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)

	mainCtx := context.Background()

	prepareEnv(logger, mainCtx, db, rdb)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		interval := plannerInterval()
		workers := plannerWorkerCount()
		const pollInterval = 1 * time.Second
		logger.VInfo("cron started, interval=%s workers=%d poll=%s", interval, workers, pollInterval)

		var inFlightMu sync.Mutex
		inFlight := make(map[string]struct{})

		var cancelMu sync.Mutex
		cancelMap := make(map[string]context.CancelFunc)

		processRunner := func(chCtx context.Context, ch models.PlayoutChannel) {
			location := utils.GetLoc()
			nowInCh := time.Now().In(location)
			today := utils.StartOfDay(nowInCh)
			endOfDay := utils.EndOfDay(nowInCh)
			tomorrow := utils.StartOfDay(nowInCh.Add(DIFF_TIME_AHEAD * time.Hour))

			isOverToday := tomorrow.After(endOfDay)

			logger.VInfo("Processing active channel: %s (%s), today=%s tomorrow=%s",
				ch.Name, ch.ID, today.Format(configs.DATE_FORMAT_YYYY_MM_DD), tomorrow.Format(configs.DATE_FORMAT_YYYY_MM_DD))

			// 1. Check if today has data (skips if present, plans if missing)
			prepareDateWithRetry(chCtx, plannerSvc, ch, today)
			if chCtx.Err() != nil {
				logger.Info(fmt.Sprintf("[Planner] channel=%s cancelled during today prepare", ch.ID))
				plannerSvc.CleanChannelData(mainCtx, ch.ID)
				return
			}

			if isOverToday {
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
			logger.VInfo("tick: %d active channel(s)", len(activeChannels))
			logMetrics(logger, mainCtx, plannerSvc, activeChannels)

			runPlannerTick(mainCtx, activeChannels, workers, &inFlightMu, inFlight, &cancelMu, cancelMap, processRunner)
		}

		nextInterval := time.Now().Add(interval)
		for {
			var activeChannels []models.PlayoutChannel
			statuses := []models.PlayoutStatus{models.PlayoutInactiveStatus, models.PlayoutFailureStatus}

			db.Where("status NOT IN ?", statuses).Find(&activeChannels)

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

	loc := utils.GetLoc()
	day := date.In(loc)
	dayStart := utils.StartOfDay(day)
	dayEnd := utils.EndOfDay(day)

	has, err := p.HasDateInTimeline(ctx, ch.ID, dayStart, dayEnd)
	if err != nil {
		logger.Error(fmt.Sprintf("[Planner] HasDateInTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), err))
	} else if has {
		logger.VInfo("[Planner] channel=%s date=%s already prepared, skipping", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
		p.ResetFailureCounterForChannel(ctx, ch.ID)
		p.MarkChannelLiveForChannel(ctx, ch.ID)
		return
	}

	prepErr := p.ProcessEPGToTimelineWithRetry(ctx, ch, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
	if prepErr != nil && ctx.Err() == nil {
		if errors.Is(prepErr, customerrors.UnspportStreamSchema) {
			p.MarkChannelFailed(ch.ID, "Unsupported stream schema")
			return
		}

		logger.Info(fmt.Sprintf("[Planner] ProcessEPGToTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), prepErr))
	}

	if ctx.Err() == nil {
		p.HandlePrepareResultForChannel(ctx, ch.ID, prepErr)
	}
}
