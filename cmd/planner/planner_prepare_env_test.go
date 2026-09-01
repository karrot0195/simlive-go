// Tests for the planner's PrepareEnv warm-start default. The planner
// must preserve all Redis keys + the on-disk cache on startup by
// default; the only way to wipe is to set ENABLE_CLEAN_UP=true.
//
// This test uses a real Redis at REDIS_ADDR (default localhost:6371)
// to verify the planner doesn't DEL simlive:timeline:* on a default
// startup. If Redis isn't reachable, the test SKIPs (following the
// project's existing pattern in internal/planner/horizon_rollover_test.go
// and internal/api/handler_test.go).

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// envGet is a tiny local wrapper around os.Getenv so this test file
// doesn't need to import a different package's helper.
func envGet(k string) string { return os.Getenv(k) }

// contextWithTimeout mirrors the production planner's pattern:
// context.WithTimeout(context.Background(), d) so the test isn't tied
// to any caller-side cancellation.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func TestPlannerPrepareEnv_DefaultPreservesTimeline(t *testing.T) {
	addr := envGet("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	ctx, cancel := contextWithTimeout(2 * time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("no Redis at %s: %v", addr, err)
	}
	defer rdb.Close()

	// Plant a sentinel timeline key.
	id := "planner-warm-start-test"
	key := "simlive:timeline:" + id
	if err := rdb.Set(ctx, key, `{"sequence":1}`, 0).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { rdb.Del(ctx, key) })

	// Default (ENABLE_CLEAN_UP unset): PrepareEnv must NOT delete it.
	t.Setenv("ENABLE_CLEAN_UP", "")
	PrepareEnv(ctx, nil, rdb)

	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("EXISTS after PrepareEnv: %v", err)
	}
	if exists != 1 {
		t.Errorf("timeline key was deleted by default PrepareEnv; want preserved")
	}
}

func TestPlannerPrepareEnv_OptInWipesEverything(t *testing.T) {
	addr := envGet("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6371"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	ctx, cancel := contextWithTimeout(2 * time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("no Redis at %s: %v", addr, err)
	}
	defer rdb.Close()

	// Plant a sentinel timeline key.
	id := "planner-cold-start-test"
	key := "simlive:timeline:" + id
	if err := rdb.Set(ctx, key, `{"sequence":1}`, 0).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { rdb.Del(ctx, key) })

	// Plant a sentinel file in TMP_DIR so we can verify the os.RemoveAll.
	dir := t.TempDir()
	t.Setenv("TMP_DIR", dir)
	sentinel := filepath.Join(dir, "seg_42.ts")
	if err := os.WriteFile(sentinel, []byte("test"), 0644); err != nil {
		t.Fatalf("plant sentinel: %v", err)
	}

	// Opt-in: ENABLE_CLEAN_UP=true -> full wipe.
	t.Setenv("ENABLE_CLEAN_UP", "true")
	PrepareEnv(ctx, nil, rdb)

	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("EXISTS after opt-in PrepareEnv: %v", err)
	}
	if exists != 0 {
		t.Errorf("timeline key survived opt-in PrepareEnv; want deleted")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("TMP_DIR sentinel survived opt-in PrepareEnv; want removed")
	}
}
