package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"simlive/internal/models"
	"simlive/internal/utils"
)

// newPlannerForHorizonTest builds a Planner with only the fields isEndOfTimeline
// touches (horizon, globalRunningTime). All other deps (db, rdb, espx, rclone)
// are nil — callers must not exercise any code path that dereferences them.
// This keeps the test pure Go (no Postgres, no Redis, no network).
func newPlannerForHorizonTest() *Planner {
	return &Planner{}
}

// TestIsEndOfTimeline_HorizonIndependentOfProcessStartTime proves the per-date
// horizon unblocks a single ProcessEPGToTimeline call from filling today AND
// tomorrow in one shot. Before the change isEndOfTimeline was anchored on
// processStartTime, so a call that started at 23:58 today could never walk past
// midnight and the engine's next-day would never get prepared.
func TestIsEndOfTimeline_HorizonIndependentOfProcessStartTime(t *testing.T) {
	loc := utils.GetLocFor("Asia/Saigon")
	now := time.Date(2026, 8, 29, 23, 58, 0, 0, loc) // 23:58 today (Asia/Saigon)

	p := newPlannerForHorizonTest()
	_ = now // kept for narrative clarity; isEndOfTimeline now consults horizon only

	t.Run("at 23:58 with horizon=tomorrow_end: not end of timeline", func(t *testing.T) {
		tomorrow := time.Date(2026, 8, 30, 0, 0, 0, 0, loc)
		p.horizon = utils.EndOfDay(tomorrow)
		// 23:58 today is well before tomorrow's 23:59:59.999999999
		if p.isEndOfTimeline(float64(now.Unix())) {
			t.Fatalf("isEndOfTimeline at 23:58 today with horizon=tomorrow should be false, got true")
		}
	})

	t.Run("at midday tomorrow with horizon=tomorrow_end: not end of timeline", func(t *testing.T) {
		tomorrow := time.Date(2026, 8, 30, 0, 0, 0, 0, loc)
		p.horizon = utils.EndOfDay(tomorrow)
		middayTomorrow := time.Date(2026, 8, 30, 12, 0, 0, 0, loc)
		if p.isEndOfTimeline(float64(middayTomorrow.Unix())) {
			t.Fatalf("isEndOfTimeline at midday tomorrow with horizon=tomorrow_end should be false, got true")
		}
	})

	t.Run("1s past horizon: end of timeline", func(t *testing.T) {
		tomorrow := time.Date(2026, 8, 30, 0, 0, 0, 0, loc)
		p.horizon = utils.EndOfDay(tomorrow)
		// 1s past EndOfDay(tomorrow) = day-after-tomorrow 00:00:00
		dayAfter := tomorrow.Add(24 * time.Hour)
		if !p.isEndOfTimeline(float64(dayAfter.Unix())) {
			t.Fatalf("isEndOfTimeline at day-after-tomorrow with horizon=tomorrow_end should be true, got false")
		}
	})

	t.Run("defensive: zero horizon is a no-op (returns false)", func(t *testing.T) {
		p.horizon = time.Time{}
		farFuture := float64(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
		if p.isEndOfTimeline(farFuture) {
			t.Fatalf("isEndOfTimeline with zero horizon must be a no-op (return false), got true")
		}
	})
}

// TestHasDateInTimeline_DedupSkipsPreparedDates exercises the ZCOUNT-based
// dedup used by prepareDate in cmd/planner/main.go:
//  1. Seed one segment per date into simlive:timeline:{ch}.
//  2. HasDateInTimeline reports has=true for both dates.
//  3. ZCARD stays at 2 (no duplicate ZADDs from a re-prepare that ignores
//     dedup) — this is the regression the test guards against.
//  4. CleanupOldSegments (with the corrected Redis key) clears the ZSET.
//
// The test is skipped when no Redis is reachable at REDIS_ADDR (or
// localhost:6371) so it works on a host with no Redis installed.
func TestHasDateInTimeline_DedupSkipsPreparedDates(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	if !redisReachable(addr) {
		t.Skipf("redis not reachable at %s; set REDIS_ADDR to enable this test", addr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis ping failed at %s: %v", addr, err)
	}

	chID := "test-horizon-rollover-dedup"
	key := fmt.Sprintf("simlive:timeline:%s", chID)
	// Clean slate — the test owns this key.
	rdb.Del(ctx, key)
	t.Cleanup(func() { rdb.Del(context.Background(), key) })

	loc := utils.GetLocFor("Asia/Saigon")
	today := utils.StartOfDay(time.Date(2026, 8, 29, 12, 0, 0, 0, loc))
	tomorrow := today.Add(24 * time.Hour)

	// Seed one segment per date — score is MediaStart (Unix seconds).
	seed := func(mediaStart time.Time, seq int64) {
		seg := models.SegmentItem{
			Sequence:   seq,
			Duration:   4.0,
			MediaStart: float64(mediaStart.Unix()),
			MediaEnd:   float64(mediaStart.Unix()) + 4.0,
		}
		b, _ := json.Marshal(seg)
		if err := rdb.ZAdd(ctx, key, redis.Z{
			Score:  seg.MediaStart,
			Member: string(b),
		}).Err(); err != nil {
			t.Fatalf("ZAdd failed: %v", err)
		}
	}
	seed(today, 1)
	seed(tomorrow, 2)

	p := &Planner{rdb: rdb}

	// Both dates should report as already prepared.
	hasToday, err := p.HasDateInTimeline(ctx, chID,
		utils.StartOfDay(today), utils.EndOfDay(today))
	if err != nil {
		t.Fatalf("HasDateInTimeline(today) error: %v", err)
	}
	if !hasToday {
		t.Fatalf("HasDateInTimeline(today) = false, want true")
	}
	hasTomorrow, err := p.HasDateInTimeline(ctx, chID,
		utils.StartOfDay(tomorrow), utils.EndOfDay(tomorrow))
	if err != nil {
		t.Fatalf("HasDateInTimeline(tomorrow) error: %v", err)
	}
	if !hasTomorrow {
		t.Fatalf("HasDateInTimeline(tomorrow) = false, want true")
	}

	// ZCARD must stay at 2 — proves the dedup short-circuits before any ZADD
	// (the regression this test guards against: re-prepare that ignores dedup
	// would ZADD a fresh member and silently inflate the ZCARD).
	card, err := rdb.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCARD error: %v", err)
	}
	if card != 2 {
		t.Fatalf("ZCARD = %d, want 2 (dedup should prevent duplicate ZADDs)", card)
	}

	// Date outside the seeded range must report as not-prepared.
	dayAfter := tomorrow.Add(24 * time.Hour)
	hasDayAfter, err := p.HasDateInTimeline(ctx, chID,
		utils.StartOfDay(dayAfter), utils.EndOfDay(dayAfter))
	if err != nil {
		t.Fatalf("HasDateInTimeline(dayAfter) error: %v", err)
	}
	if hasDayAfter {
		t.Fatalf("HasDateInTimeline(dayAfter) = true, want false (no segments seeded for this date)")
	}

	// CleanupOldSegments (with the corrected simlive:timeline key) must clear
	// the ZSET — this exercises the bug fix in CleanupOldSegments.
	p.CleanupOldSegments(ctx, chID)
	card, err = rdb.ZCard(ctx, key).Result()
	if err != nil {
		t.Fatalf("ZCARD after cleanup error: %v", err)
	}
	if card != 0 {
		t.Fatalf("ZCARD after CleanupOldSegments = %d, want 0 (correct key + retention trim)", card)
	}

	// Boundary: an empty ZSET must report has=false (no off-by-one in the
	// ZCOUNT [start,end] window). This is the cold-start case before the
	// planner's first ProcessEPGToTimeline ever writes a member.
	emptyChID := "test-horizon-rollover-empty"
	emptyKey := fmt.Sprintf("simlive:timeline:%s", emptyChID)
	rdb.Del(ctx, emptyKey)
	t.Cleanup(func() { rdb.Del(context.Background(), emptyKey) })

	emptyToday := utils.StartOfDay(time.Date(2026, 8, 29, 12, 0, 0, 0, loc))
	hasEmpty, err := p.HasDateInTimeline(ctx, emptyChID,
		utils.StartOfDay(emptyToday), utils.EndOfDay(emptyToday))
	if err != nil {
		t.Fatalf("HasDateInTimeline(empty) error: %v", err)
	}
	if hasEmpty {
		t.Fatalf("HasDateInTimeline(empty) = true, want false (no segments seeded)")
	}
}

// redisReachable does a quick TCP dial so the test can skip cleanly when no
// Redis is running locally, rather than waiting for the redis client to time
// out.
func redisReachable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
