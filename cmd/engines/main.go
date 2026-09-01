package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"simlive/internal/engine"
	"simlive/internal/models"
	"simlive/internal/storage"
	"simlive/internal/utils"
)

// PrepareEnv is a best-effort startup hook for the engine binary.
//
// History: the engine previously did `os.RemoveAll(tmpDir)` on every
// startup, blowing away the bounded active on-disk window
// (`{TMP_DIR}/{ch}/seg_N.ts` + `.pts` markers). The rationale was to
// guarantee a clean slate — but the cost was high: every channel the
// engine served had to re-download all 15 segments via rclone after
// every engine restart. On a fleet restart (or a binary deploy), that
// meant tens of seconds of "no data" for every channel.
//
// New behavior: by default the on-disk cache is **preserved** across
// restarts so the engine can resume immediately on whatever the
// previous run had already downloaded. The `syncLocalCacheDir` pass on
// the first tick of the loop will evict any segments that no longer
// fit the new active window. To force a cold start (e.g. after a
// storage change, or in CI), set `ENABLE_CLEAN_UP=true` — the engine
// will then nuke the cache on startup as before.
//
// Redis is intentionally NOT touched here. The HLS playlist keys
// (`simlive:hls:*`) are written by the engine itself and the
// `purgeChannelRedisData` call in `StopChannel` / `DeleteChannel`
// already handles the "stop" case. The timeline + renditions + cursor
// keys are written by the planner and survive engine restarts by
// design.
func PrepareEnv(ctx context.Context, rdb *redis.Client) {
	cleanUp := utils.GetEnv("ENABLE_CLEAN_UP", "") == "true"
	if !cleanUp {
		log.Printf("[Engine] startup: preserving {TMP_DIR} cache (set ENABLE_CLEAN_UP=true for a cold start)")
		return
	}
	tmpDir := utils.GetEnv("TMP_DIR", os.TempDir())
	log.Printf("[Engine] startup: ENABLE_CLEAN_UP=true, removing %s", tmpDir)
	os.RemoveAll(tmpDir)
}

func main() {
	godotenv.Load()

	settings := utils.GetSettings()
	maxWorkers, _ := strconv.Atoi(utils.GetEnv("MAX_WORKERS", "10"))

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
	rcloneSvc := storage.NewRcloneService()

	ctx := context.Background()

	engineSvc := engine.NewEngine(rdb, rcloneSvc, maxWorkers)

	PrepareEnv(ctx, rdb)
	var wg sync.WaitGroup

	wg.Add(1)
	// 5. Engine Loop: ONLY STREAM FOR CHANNELS WITH is_active = true AND
	//    status != 'INACTIVE' (i.e. PREPARE or LIVE).
	//
	// Lifecycle gate: a channel is created/started by the REST surface in
	// status 'PREPARE' (is_active=true). The engine starts the playout loop
	// IMMEDIATELY on PREPARE — it does NOT wait for the planner to flip to
	// 'LIVE'. The loop will idle (no segments to serve) until the planner
	// writes timeline data to Redis. The planner is the SINGLE writer of
	// 'LIVE' (markChannelLive / MarkChannelLiveForChannel in
	// internal/planner/planner.go); the engine only gates on it and never
	// writes status. Legacy rows stuck in the removed 'ACTIVE' state
	// self-heal on the next planner tick (a successful prepare or a "date
	// already prepared" short-circuit flips them to 'LIVE').
	//
	// Concurrency model: ONE goroutine per active channel. The reconciler
	// polls the DB on a short cadence (5s), and on every tick:
	//   - starts a new playout loop for any channel that is `is_active`
	//     AND `status='LIVE'` and doesn't have one yet;
	//   - cancels the playout loop for any tracked channel that's no
	//     longer in the active set (channel was stopped / soft-deleted
	//     via the REST surface, or is still PREPARE) and removes its
	//     entry from the map.
	//
	// This invariant — "at most one running engine per channel id" — is
	// the engine half of the "one channel = one planner + one engine"
	// guarantee. Without the cancel-and-prune step, stopping a channel
	// would leak its goroutine and its {TMP_DIR}/{ch} directory forever.
	go func() {
		time.Sleep(3 * time.Second)
		activeEngineTasks := make(map[string]context.CancelFunc)
		// tasksMu guards activeEngineTasks. The reconciler reads+mutates
		// it on every tick; engine goroutines only invoke the stored
		// CancelFunc (which is itself safe to call from any goroutine),
		// so the only contention is reconciler-vs-reconciler, which is
		// serial anyway because this is a single goroutine. The mutex
		// is here to make that explicit (and to keep the linter happy
		// if we ever fan out the reconciler).
		var tasksMu sync.Mutex
		defer wg.Done()
		// The spawn callback is what the production engine loop will
		// actually do with a freshly-allocated (ctx, cancel) pair. The
		// reconciler owns the cancel's lifetime; the callback is just
		// a "you've been allocated, go" hook.
		spawn := func(channelID string, channelCtx context.Context) {
			go engineSvc.StartPlayoutLoop(channelCtx, channelID)
		}
		for {
			var activeChannels []models.PlayoutChannel
			// Engine starts on PREPARE or LIVE (any non-INACTIVE status).
			// The planner still flips PREPARE->LIVE when timeline data is ready,
			// but the engine doesn't wait for it — it begins the playout loop
			// immediately and will idle until Redis has timeline data.
			db.Where("is_active = ? AND status != ?", true, "INACTIVE").Find(&activeChannels)

			activeIDs := make([]string, 0, len(activeChannels))
			for _, ch := range activeChannels {
				activeIDs = append(activeIDs, ch.ID)
			}

			reconcileEngines(ctx, activeIDs, &activeEngineTasks, &tasksMu, spawn)

			time.Sleep(5 * time.Second)
		}
	}()

	wg.Wait()
}

// reconcileEngines enforces the "at most one engine per channel id"
// invariant. On every call it:
//
//  1. Cancels the playout loop for any tracked channel that's no longer
//     in `activeIDs` and removes its entry from `tasks` (so the entry
//     can be re-spawned later if the channel is re-started).
//  2. Spawns a new playout loop for every active channel that isn't
//     already tracked (dedup via the map). A fresh
//     `context.WithCancel(parent)` is allocated for each new entry and
//     its cancel func is stored in the map before `spawn` is invoked.
//
// `tasks` and `tasksMu` are owned by the caller; `parent` is the
// long-lived context the new per-channel contexts are derived from.
// `spawn` is invoked synchronously from this function for every
// newly-tracked channel; the production `spawn` launches
// `engineSvc.StartPlayoutLoop` in its own goroutine.
//
// Exported at package scope so the unit test (cmd/engines/main_test.go)
// can drive the reconciler with a stub spawn and assert on the diff.
func reconcileEngines(
	parent context.Context,
	activeIDs []string,
	tasks *map[string]context.CancelFunc,
	tasksMu *sync.Mutex,
	spawn func(channelID string, channelCtx context.Context),
) {
	activeSet := make(map[string]struct{}, len(activeIDs))
	for _, id := range activeIDs {
		activeSet[id] = struct{}{}
	}

	// Two-phase commit: collect the spawns to run under the lock, then
	// invoke them outside the lock. This keeps the lock window tight
	// (map mutations only) and lets test spawn functions safely read
	// the map without deadlocking.
	var toSpawn []newSpawn

	tasksMu.Lock()
	// 1. Cancel + prune anything that left the active set.
	for id, cancel := range *tasks {
		if _, stillActive := activeSet[id]; !stillActive {
			log.Printf("[Engine] Stopping playout engine for channel: %s (no longer active)", id)
			cancel()
			delete(*tasks, id)
		}
	}
	// 2. Pre-allocate (ctx, cancel) for anything new. The map is the
	//    single source of truth for "who's running", so a second spawn
	//    for the same id is impossible by construction.
	for _, id := range activeIDs {
		if _, running := (*tasks)[id]; !running {
			log.Printf("[Engine] Starting playout engine for channel: %s", id)
			channelCtx, cancel := context.WithCancel(parent)
			(*tasks)[id] = cancel
			toSpawn = append(toSpawn, newSpawn{id: id, ctx: channelCtx})
		}
	}
	tasksMu.Unlock()

	// 3. Run the spawns outside the lock.
	for _, s := range toSpawn {
		spawn(s.id, s.ctx)
	}
}

type newSpawn struct {
	id  string
	ctx context.Context
}
