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

// PrepareEnv is a best-effort startup hook for the planner binary.
//
// History:
//  1. The planner previously did `rdb.FlushAll(ctx)` on startup —
//     wiped every key in Redis, including the engine's HLS playlists.
//  2. Then a scoped wipe of just `simlive:timeline:*` — better, but
//     still caused the engine to lose its serving state for every
//     channel whose timeline was wiped, until the planner rebuilt it
//     from scratch (~30s–2min per channel on cold start).
//
// New behavior: by default the planner **preserves** all Redis keys
// and the on-disk cache. The `HasDateInTimeline` short-circuit in
// `ProcessEPGToTimeline` already prevents redundant re-preparation
// of a date that already has data — so there's no benefit to wiping
// the timeline on startup, and the cost was real: any channel whose
// timeline was wiped had to wait through a full cold-start EPG fetch
// + rclone cat + Redis pipeline before the engine could serve
// HLS playlists again.
//
// To force a full wipe (e.g. in CI when you want a clean Redis), set
// `ENABLE_CLEAN_UP=true` — the planner falls back to the previous
// `FlushAll` + `os.RemoveAll(tmpDir)` behavior.
func PrepareEnv(ctx context.Context, db *gorm.DB, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if cleanUp {
		log.Printf("[Planner] startup: ENABLE_CLEAN_UP=true, flushing all Redis keys + removing TMP_DIR")
		if rdb != nil {
			rdb.FlushAll(ctx)
		}
		tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
		os.RemoveAll(tmpDir)
		// When Redis is wiped, all active channels lose their timeline data.
		// Reset their DB status to PREPARE so the planner rebuilds from scratch
		// and the engine stops serving stale playlists until LIVE is restored.
		if db != nil {
			db.Model(&models.PlayoutChannel{}).
				Where("is_active = ?", true).
				Update("status", "PREPARE")
		}
		return
	}
	log.Printf("[Planner] startup: preserving all Redis keys + TMP_DIR cache (set ENABLE_CLEAN_UP=true for a cold start)")
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
// channels in parallel on each tick. Override via PLANNER_WORKERS.
//
// ProcessEPGToTimeline is safe to call concurrently for DIFFERENT channels:
// the per-call walk state (runningTime, totalSegmentsAdded, channelID,
// horizon) lives on the SegmentProcessor, not the shared Planner (see the
// NOTE on the Planner struct in internal/planner/planner.go). The per-channel
// in-flight guard in runPlannerTick still prevents overlapping calls for the
// SAME channel. Default is 4 so a multi-channel fleet fills its timelines in
// parallel (the old epgMu + clamp-to-1 design serialized channels one at a
// time, so with 2 active channels the second had no data for the whole
// cold-start window). Values < 1 are floored to 1.
func plannerWorkerCount() int {
	const defaultWorkers = 4
	n := utils.GetEnvInt("PLANNER_WORKERS", defaultWorkers)
	if n < 1 {
		return 1
	}
	return n
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
		log.Printf("[METRIC] channel=%s name=%q zset_size=%d cursor=%s live_segment=%d download_ahead=%d",
			ch.ID, ch.Name, size, cursorStr, cursor, 5)
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Failed to load .env file: %v", err)
	}

	settings := utils.GetSettings()

	// 1. Connect Postgres
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		settings.DBHost, settings.DBUser, settings.DBPassword, settings.DBName, settings.DBPort)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect DB: %v", err)
	}

	if err := db.AutoMigrate(&models.PlayoutChannel{}); err != nil {
		log.Fatalf("Failed to migrate DB: %v", err)
	}

	// 2. Connect Redis
	rdb := redis.NewClient(&redis.Options{Addr: settings.RedisAddr})

	// 3. Init Services
	espxClient := api.NewESPXClient(settings.EspxURL)
	rcloneSvc := storage.NewRcloneService()
	plannerSvc := planner.NewPlanner(db, rdb, espxClient, rcloneSvc)

	ctx := context.Background()

	PrepareEnv(ctx, db, rdb)

	var wg sync.WaitGroup

	wg.Add(1)
	// 4. Planner Cron Loop: today + tomorrow, per-channel content timezone.
	//    Each tick short-circuits dates that HasDateInTimeline already covers.
	//    Channels are processed concurrently by a bounded worker pool
	//    (PLANNER_WORKERS); per-(channel,date) EPG fetches use bounded
	//    exponential backoff via ProcessEPGToTimelineWithRetry.
	//
	// Concurrency model: "one channel = one planner worker, and at most one
	// in-flight worker per channel id at any time". Different channels may
	// run concurrently up to PLANNER_WORKERS because per-call walk state
	// (runningTime, totalSegmentsAdded, channelID, horizon) lives on the
	// SegmentProcessor, not the shared Planner. The inFlightMu registry
	// additionally prevents overlapping ticks for the SAME channel: if a
	// tick fires while the previous worker for the same channel is still
	// running, the new tick skips the channel (logged) and lets the
	// in-flight worker finish — the next tick will pick it up. This
	// prevents the pile-up of overlapping workers for a slow channel.
	go func() {
		defer wg.Done()
		interval := plannerInterval()
		workers := plannerWorkerCount()
		log.Printf("[Planner] cron started, interval=%s workers=%d", interval, workers)

		// Per-channel in-flight registry. Held by a worker for the duration
		// of its processing; released in a defer. If a tick fires while the
		// previous worker for the same channel is still running, the new
		// tick skips the channel (logged) and lets the in-flight worker
		// finish — the next tick will pick it up. This prevents the
		// pile-up of overlapping workers for a slow channel.
		var inFlightMu sync.Mutex
		inFlight := make(map[string]struct{})

		// The process callback is what the production loop does for one
		// channel on one tick. It's defined here so it can close over
		// `plannerSvc` + `ctx` without the reconciler needing to know
		// about planner internals.
		process := func(ch models.PlayoutChannel) {
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
		}

		for {
			var activeChannels []models.PlayoutChannel
			db.Where("is_active = ?", true).Find(&activeChannels)

			log.Printf("[Planner] tick: %d active channel(s)", len(activeChannels))

			// §7 — emit per-channel metrics for the previous tick window.
			logMetrics(ctx, plannerSvc, activeChannels)

			runPlannerTick(activeChannels, workers, &inFlightMu, inFlight, process)

			time.Sleep(interval)
		}
	}()

	wg.Wait()
}

// runPlannerTick executes one planner tick: each active channel is run
// in its own goroutine under a bounded worker pool, with a per-channel
// in-flight guard preventing ProcessEPGToTimeline from being called
// concurrently for the same channel.
//
// `inFlight` and `inFlightMu` are owned by the caller and persist
// across ticks (they're the long-lived registry of "channels whose
// previous tick is still running"). On every call, the function:
//
//  1. Iterates `activeChannels` and launches a worker per channel.
//  2. Each worker tries to claim the in-flight slot for its channel.
//     If the slot is held (a previous tick is still running), the
//     worker logs and returns immediately — the in-flight worker will
//     release the slot when it finishes, and the next tick will
//     re-attempt the channel.
//  3. If the slot is claimed, the worker calls `process(ch)` and then
//     releases the slot in a defer.
//
// Cross-channel parallelism is capped at `workers` (via a semaphore
// channel). Exported at package scope so the unit test
// (cmd/planner/main_test.go) can drive the reconciler with a stub
// process callback and assert on the in-flight semantics.
func runPlannerTick(
	activeChannels []models.PlayoutChannel,
	workers int,
	inFlightMu *sync.Mutex,
	inFlight map[string]struct{},
	process func(ch models.PlayoutChannel),
) {
	sem := make(chan struct{}, workers)
	var tickWG sync.WaitGroup
	for _, ch := range activeChannels {
		tickWG.Add(1)
		sem <- struct{}{}
		go func(ch models.PlayoutChannel) {
			defer tickWG.Done()
			defer func() { <-sem }()

			// Per-channel "only one worker in flight" guard.
			inFlightMu.Lock()
			if _, busy := inFlight[ch.ID]; busy {
				inFlightMu.Unlock()
				log.Printf("[Planner] channel=%s still in flight from previous tick, skipping", ch.ID)
				return
			}
			inFlight[ch.ID] = struct{}{}
			inFlightMu.Unlock()
			defer func() {
				inFlightMu.Lock()
				delete(inFlight, ch.ID)
				inFlightMu.Unlock()
			}()

			process(ch)
		}(ch)
	}
	tickWG.Wait()
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
			log.Printf("[Planner] channel=%s date=%s already prepared, skipping (data already in Redis => status flips to LIVE)", ch.ID, dayStart.Format("2006-01-02"))
			p.ResetFailureCounterForChannel(ctx, ch.ID)
			p.MarkChannelLiveForChannel(ctx, ch.ID)
			return
		}
	}

	prepErr := p.ProcessEPGToTimelineWithRetry(ctx, ch, dayStart.Format("2006-01-02"))
	if prepErr != nil {
		log.Printf("[Planner] ProcessEPGToTimeline(%s, %s) error after retries: %v", ch.ID, dayStart.Format("2006-01-02"), prepErr)
	}
	p.HandlePrepareResultForChannel(ctx, ch.ID, prepErr)
}
