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
	"simlive/internal/lifecycle"
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

const DIFF_TIME_AHEAD = 3

var logger = utils.GetLogger("PLANNER")

type supervisor struct {
	ch         models.PlayoutChannel
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	state      string // "running" | "stopping"
	restartCh  models.PlayoutChannel
	hasRestart bool
}

func main() {
	if err := godotenv.Load(); err != nil {
		logger.VWarning("Failed to load .env file: %v", err)
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

	partnerClient := api.NewPartnerClient()
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, partnerClient, rcloneSvc)

	mainCtx := context.Background()

	prepareEnv(logger, mainCtx, db, rdb)

	interval := plannerInterval()
	workers := plannerWorkerCount()
	pollInterval := utils.GetChannelSyncInterval()
	logger.VInfo("cron started, interval=%s workers=%d poll=%s", interval, workers, pollInterval)

	sem := make(chan struct{}, workers)

	processRunner := func(chCtx context.Context, ch models.PlayoutChannel) {
		location := utils.LocalLocation()
		nowInCh := time.Now().In(location)
		today := utils.StartOfDay(nowInCh)
		tomorrow := today.Add(24 * time.Hour)

		isOverToday := nowInCh.After(tomorrow.Add(-DIFF_TIME_AHEAD * time.Hour))

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

		logger.Info("[PLANER] processed", "channelId", ch.ID)

		// 3. Clean up expired data AFTER tick processing completes
		plannerSvc.CleanupOldSegments(chCtx, ch.ID)
	}

	startSupervisor := func(ch models.PlayoutChannel) *supervisor {
		supCtx, cancel := context.WithCancel(mainCtx)
		sup := &supervisor{
			ch:     ch,
			ctx:    supCtx,
			cancel: cancel,
			done:   make(chan struct{}),
			state:  "running",
		}
		go func(s *supervisor) {
			defer close(s.done)
			for {
				// acquire worker slot (never blocks discovery)
				select {
				case sem <- struct{}{}:
				case <-s.ctx.Done():
					return
				}
				processRunner(s.ctx, s.ch)
				<-sem

				if s.ctx.Err() != nil {
					return
				}

				// wait for next plan interval or cancellation
				select {
				case <-time.After(interval):
				case <-s.ctx.Done():
					return
				}

				if s.ctx.Err() != nil {
					return
				}
			}
		}(sup)
		return sup
	}

	supervisors := make(map[string]*supervisor)
	var mu sync.Mutex

	// boot: immediate reconcile
	logger.VInfo("boot reconcile")

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	reconcile := func() {
		mu.Lock()
		defer mu.Unlock()

		activeChannels, err := lifecycle.ActiveChannels(db)
		if err != nil {
			logger.Error("[Planner] ActiveChannels error", "error", err)
			return
		}
		activeByID := make(map[string]models.PlayoutChannel, len(activeChannels))
		for _, ch := range activeChannels {
			activeByID[ch.ID] = ch
		}

		// 1. Handle stopping supervisors that have exited.
		for id, sup := range supervisors {
			if sup.state != "stopping" {
				continue
			}
			select {
			case <-sup.done:
				if sup.hasRestart {
					ch := sup.restartCh
					newSup := startSupervisor(ch)
					supervisors[id] = newSup
					logger.Info("[Planner] restarted supervisor after exit", "channel", id)
				} else if _, ok := activeByID[id]; ok {
					newSup := startSupervisor(activeByID[id])
					supervisors[id] = newSup
					logger.Info("[Planner] restarted supervisor (still active)", "channel", id)
				} else {
					plannerSvc.CleanChannelData(mainCtx, id)
					delete(supervisors, id)
					logger.Info("[Planner] cleaned up stopped channel", "channel", id)
				}
			default:
				// still stopping
			}
		}

		// 2. Mark running supervisors for stop / restart.
		for id, sup := range supervisors {
			if sup.state != "running" {
				continue
			}
			ch, ok := activeByID[id]
			if !ok {
				// removed
				sup.state = "stopping"
				sup.cancel()
				logger.Info("[Planner] channel removed/deactivated, cancelling", "channel", id)
				continue
			}
			if !models.ChannelDefinitionsEqual(sup.ch, ch) {
				// definition changed -> restart
				sup.state = "stopping"
				sup.cancel()
				sup.restartCh = ch
				sup.hasRestart = true
				logger.Info("[Planner] channel definition changed, restarting", "channel", id)
			}
		}

		// 3. Start supervisors for newly active channels.
		for _, ch := range activeChannels {
			if _, ok := supervisors[ch.ID]; !ok {
				supervisors[ch.ID] = startSupervisor(ch)
				logger.Info("[Planner] started supervisor for new active channel", "channel", ch.ID)
			}
		}
	}

	// immediate first pass
	reconcile()

	for range ticker.C {
		reconcile()
	}
}

func prepareDateWithRetry(ctx context.Context, p *planner.Planner, ch models.PlayoutChannel, date time.Time) {
	if err := ctx.Err(); err != nil {
		return
	}

	loc := utils.LocalLocation()
	day := date.In(loc)
	dayStart := utils.StartOfDay(day)
	dayEnd := utils.EndOfDay(day)

	has, err := p.HasDateInTimeline(ctx, ch.ID, dayStart, dayEnd)
	if err != nil {
		logger.Error(fmt.Sprintf("[Planner] HasDateInTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), err))
	} else if has {
		recovered, recErr := p.RecoverFallbackDay(ctx, ch, dayStart)
		if recErr != nil {
			logger.VError("[Planner] RecoverFallbackDay(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), recErr)
		} else if recovered {
			logger.VInfo("[Planner] channel=%s date=%s recovered from fallback with provider data", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
		} else {
			logger.VInfo("[Planner] channel=%s date=%s already prepared, skipping", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
		}
		p.ResetFailureCounterForChannel(ctx, ch.ID)
		p.MarkChannelLiveForChannel(ctx, ch.ID)
		return
	}

	prepErr := p.ProcessEPGToTimelineWithRetry(ctx, ch, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD))
	if prepErr != nil && ctx.Err() == nil {
		if errors.Is(prepErr, customerrors.UnspportStreamSchema) {
			p.MarkChannelFailed(ctx, ch.ID, "Unsupported stream schema")
			return
		}

		logger.Info(fmt.Sprintf("[Planner] ProcessEPGToTimeline(%s, %s) error: %v", ch.ID, dayStart.Format(configs.DATE_FORMAT_YYYY_MM_DD), prepErr))
	}

	if ctx.Err() == nil {
		p.HandlePrepareResultForChannel(ctx, ch.ID, prepErr)
	}
}
