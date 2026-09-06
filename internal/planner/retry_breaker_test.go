package planner

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newRetryPlanner builds a Planner wired to a Redis reachable at REDIS_ADDR
// (default localhost:6371). Returns the planner and a cancel-able context.
// Skips the test when no Redis is reachable.
func newRetryPlanner(t *testing.T) (*Planner, context.Context, func()) {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithCancel(context.Background())

	// Ping with a short timeout so the test skips quickly when Redis is absent.
	pingCtx, pingCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer pingCancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		cancel()
		_ = rdb.Close()
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	return &Planner{rdb: rdb}, ctx, func() {
		cancel()
		_ = rdb.Close()
	}
}

// TestFailureCounter_SuccessResetsZero verifies that a failed prepare
// followed by a successful prepare drops the per-channel failure counter
// back to 0. This is what guarantees a transiently-broken EPG endpoint
// doesn't accumulate failures forever.
func TestFailureCounter_SuccessResetsZero(t *testing.T) {
	p, ctx, cleanup := newRetryPlanner(t)
	defer cleanup()

	chID := "test-retry-counter-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() { p.rdb.Del(ctx, failureKey(chID)) })

	if got := p.incrFailureCounter(ctx, chID); got != 1 {
		t.Fatalf("first incr: want 1, got %d", got)
	}
	if got := p.incrFailureCounter(ctx, chID); got != 2 {
		t.Fatalf("second incr: want 2, got %d", got)
	}

	p.handlePrepareResult(ctx, chID, errors.New("boom"))
	if got := p.getFailureCounter(ctx, chID); got != 3 {
		t.Fatalf("after handlePrepareResult(err): want 3, got %d", got)
	}

	p.handlePrepareResult(ctx, chID, nil)
	if got := p.getFailureCounter(ctx, chID); got != 0 {
		t.Fatalf("after handlePrepareResult(nil): want 0, got %d", got)
	}
}

// TestRetryBackoffDelay_Schedule verifies the exponential-backoff schedule
// is monotonic up to the cap. base=100ms, cap=1s:
//
//	i=1 → 100ms, i=2 → 200ms, i=3 → 400ms, i=4 → 800ms, i=5 → 1s (capped),
//	i=6 → 1s (capped), i=0 → 0 (defensive).
func TestRetryBackoffDelay_Schedule(t *testing.T) {
	base := 100 * time.Millisecond
	max := time.Second

	cases := []struct {
		i    int
		want time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{4, 800 * time.Millisecond},
		{5, time.Second},  // capped
		{6, time.Second},  // still capped
		{20, time.Second}, // way past cap
		{0, 0},            // defensive
		{-1, 0},           // defensive
	}
	for _, tc := range cases {
		if got := retryBackoffDelay(tc.i, base, max); got != tc.want {
			t.Errorf("retryBackoffDelay(%d) = %s, want %s", tc.i, got, tc.want)
		}
	}
}

// TestMarkChannelInactive_RespectsDB verifies that markChannelInactive
// short-circuits cleanly when no *gorm.DB is wired up (i.e. in unit tests).
// Without this guard a nil-deref would crash on the first breaker trip.
func TestMarkChannelInactive_RespectsDB(t *testing.T) {
	p := &Planner{} // db is nil
	if err := p.markChannelInactive("any-channel"); err != nil {
		t.Fatalf("markChannelInactive with nil db should be a no-op, got %v", err)
	}
}

// TestMarkChannelLive_RespectsDB verifies that markChannelLive short-circuits
// cleanly when no *gorm.DB is wired up (i.e. in unit tests). Without this
// guard a nil-deref would crash on the first planner success — and
// TestFailureCounter_SuccessResetsZero above calls handlePrepareResult(nil)
// with a nil-db planner, which must keep passing as a no-op.
func TestMarkChannelLive_RespectsDB(t *testing.T) {
	p := &Planner{} // db is nil
	if err := p.markChannelLive("any-channel"); err != nil {
		t.Fatalf("markChannelLive with nil db should be a no-op, got %v", err)
	}
}

// failureKey exposes the per-channel failure counter key for tests so we can
// clean up the Redis key after each test (the planner doesn't expose a
// public Delete helper for it).
func failureKey(channelID string) string {
	return "planner:failures:" + channelID
}
