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
	"simlive/internal/models"
	"simlive/internal/planner"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
	// clean up
	rdb.FlushAll(ctx)
	os.RemoveAll(tmpDir)
}

// plannerInterval returns the cron sleep between planner ticks.
// Defaults to 5m; override via PLANNER_INTERVAL (e.g. "1m", "30s", "1h").
// A zero/negative duration falls back to 5m.
func plannerInterval() time.Duration {
	d, err := time.ParseDuration(utils.GetEnv("PLANNER_INTERVAL", "5m"))
	if err != nil || d <= 0 {
		return 5 * time.Minute
	}
	return d
}

// plannerWorkerCount returns the number of goroutines that process active
// channels in parallel on each tick. Override via PLANNER_WORKERS (default 4).
// A non-positive value falls back to the default.
func plannerWorkerCount() int {
	return utils.GetEnvInt("PLANNER_WORKERS", 4)
}

// logMetrics emits one structured log line per channel with the current
// Redis ZSET size, last-processed cursor, and consecutive-failure counter.
// Designed to be greppable + cheap so an external scraper (Loki, etcd, …)
// can pick it up; no Prometheus client added yet.
func logMetrics(ctx context.Context, p *planner.Planner, channels []models.PlayoutChannel) {
	for _, ch := range channels {
		size := p.TimelineSize(ctx, ch.ID)
		cursor := p.LastProcessedCursor(ctx, ch.ID)
		cursorStr := "none"
		if cursor > 0 {
			cursorStr = time.Unix(cursor, 0).UTC().Format("2006-01-02T15:04:05Z")
		}
		log.Printf("[METRIC] channel=%s name=%q zset_size=%d cursor=%s",
			ch.ID, ch.Name, size, cursorStr)
	}
}

func main() {
	godotenv.Load()

	settings := utils.GetSettings()

	// 1. Connect Postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}
	db.AutoMigrate(&models.PlayoutChannel{})

	// 2. Connect Redis
	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	// 3. Init Services
	espxClient := api.NewESPXClient(settings.EspxURL)
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)

	ctx := context.Background()

	PrepareEnv(ctx, rdb)

	var wg sync.WaitGroup

	wg.Add(1)
	// 4. Planner Cron Loop: today + tomorrow, per-channel content timezone.
	//    Each tick short-circuits dates that HasDateInTimeline already covers.
	//    Channels are processed concurrently by a bounded worker pool
	//    (PLANNER_WORKERS); per-(channel,date) EPG fetches use bounded
	//    exponential backoff via ProcessEPGToTimelineWithRetry.
	go func() {
		defer wg.Done()
		interval := plannerInterval()
		workers := plannerWorkerCount()
		log.Printf("[Planner] cron started, interval=%s workers=%d", interval, workers)

		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)

			log.Printf("[Planner] tick: %d active channel(s)", len(activeChannels))

			// §7 — emit per-channel metrics for the previous tick window.
			logMetrics(ctx, plannerSvc, activeChannels)

			// §4 — bounded worker pool: at most `workers` channels processed
			// in parallel. A semaphore channel caps concurrency; a WaitGroup
			// joins all workers before the tick sleeps.
			sem := make(chan struct{}, workers)
			var tickWG sync.WaitGroup
			for _, ch := range activeChannels {
				tickWG.Add(1)
				sem <- struct{}{}
				go func(ch models.PlayoutChannel) {
					defer tickWG.Done()
					defer func() { <-sem }()

					loc := utils.GetLocFor(ch.ContentTimezone)
					nowInCh := time.Now().In(loc)
					today := utils.StartOfDay(nowInCh)
					tomorrow := today.Add(24 * time.Hour)

					log.Printf("[Planner] Processing active channel: %s (%s), today=%s tomorrow=%s",
						ch.Name, ch.ID, today.Format("2006-01-02"), tomorrow.Format("2006-01-02"))

					// prepareDateWithRetry wraps prepareDate with retry/backoff
					// and records success/failure into the per-channel counter.
					prepareDateWithRetry(ctx, plannerSvc, ch, today)
					prepareDateWithRetry(ctx, plannerSvc, ch, tomorrow)

					// Bump the per-channel cursor to the engine's lookahead
					// horizon and trim the ZSET to RETENTION_SECONDS.
					if err := plannerSvc.ProcessWindow(ctx, ch); err != nil {
						log.Printf("[Planner] ProcessWindow(%s) error: %v", ch.ID, err)
					}
					plannerSvc.CleanupOldSegments(ctx, ch.ID)
				}(ch)
			}
			tickWG.Wait()

			time.Sleep(interval)
		}
	}()

	wg.Wait()
}

// prepareDateWithRetry is the retry-aware version of prepareDate. It uses
// ProcessEPGToTimelineWithRetry for bounded exponential backoff, and feeds
// each attempt's outcome into Planner.handlePrepareResult so the inactivity
// breaker (§7) can deactivate consistently-failing channels.
func prepareDateWithRetry(ctx context.Context, p *planner.Planner, ch models.PlayoutChannel, date time.Time) {
	loc := utils.GetLocFor(ch.ContentTimezone)
	day := date.In(loc)
	dayStart := utils.StartOfDay(day)
	dayEnd := utils.EndOfDay(day)

	has, err := p.HasDateInTimeline(ctx, ch.ID, dayStart, dayEnd)
	if err != nil {
		log.Printf("[Planner] HasDateInTimeline(%s, %s) error: %v (will re-prepare)", ch.ID, dayStart.Format("2006-01-02"), err)
	} else if has {
		// M1 upgrade guard: if the ZSET already has data but it lacks the
		// "rendition" tag (pre-M1 timeline), DEL and re-prepare so members
		// are written with the new field. See
		// docs/300826-multi-rendition-abr-ladder.md §3 "Legacy timeline flush".
		if legacy, lerr := p.HasLegacyTimeline(ctx, ch.ID); lerr == nil && legacy {
			log.Printf("[Planner] channel=%s date=%s has legacy timeline (no rendition tag), flushing for M1 upgrade",
				ch.ID, dayStart.Format("2006-01-02"))
			if delErr := p.DeleteTimeline(ctx, ch.ID); delErr != nil {
				log.Printf("[Planner] DEL legacy timeline(%s) error: %v", ch.ID, delErr)
			}
		} else {
			log.Printf("[Planner] channel=%s date=%s already prepared, skipping", ch.ID, dayStart.Format("2006-01-02"))
			p.ResetFailureCounterForChannel(ctx, ch.ID)
			return
		}
	}

	prepErr := p.ProcessEPGToTimelineWithRetry(ctx, ch, dayStart.Format("2006-01-02"))
	if prepErr != nil {
		log.Printf("[Planner] ProcessEPGToTimeline(%s, %s) error after retries: %v", ch.ID, dayStart.Format("2006-01-02"), prepErr)
	}
	p.HandlePrepareResultForChannel(ctx, ch.ID, prepErr)
}
