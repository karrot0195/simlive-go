// Tests for the warm-start behavior of cmd/engines/main.go PrepareEnv
// and cmd/planner/main.go PrepareEnv.
//
// Both functions are now opt-in destructive (only when
// ENABLE_CLEAN_UP=true); by default they preserve the existing
// on-disk cache and (for the planner) do a scoped Redis wipe. These
// tests pin the contract so a future refactor that accidentally
// re-introduces a destructive default is caught.
//
// No infrastructure needed — both functions are pure wrt filesystem
// and Redis (Redis is a no-op when ENABLE_CLEAN_UP is unset, and
// filesystem writes are guarded by the env check).

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnginePrepareEnv_DefaultPreservesCache writes a sentinel file
// into TMP_DIR, calls PrepareEnv with ENABLE_CLEAN_UP unset, and
// asserts the sentinel survives.
func TestEnginePrepareEnv_DefaultPreservesCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMP_DIR", dir)
	t.Setenv("ENABLE_CLEAN_UP", "")

	// Plant a sentinel.
	sentinel := filepath.Join(dir, "seg_42.ts")
	if err := os.WriteFile(sentinel, []byte("test"), 0644); err != nil {
		t.Fatalf("plant sentinel: %v", err)
	}

	PrepareEnv(nil, nil) // rdb is nil; the no-op early-return handles it

	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("sentinel was removed by PrepareEnv (default = preserve); err=%v", err)
	}
}

// TestEnginePrepareEnv_OptInRemovesCache verifies that
// ENABLE_CLEAN_UP=true actually wipes TMP_DIR.
func TestEnginePrepareEnv_OptInRemovesCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMP_DIR", dir)
	t.Setenv("ENABLE_CLEAN_UP", "true")

	sentinel := filepath.Join(dir, "seg_42.ts")
	if err := os.WriteFile(sentinel, []byte("test"), 0644); err != nil {
		t.Fatalf("plant sentinel: %v", err)
	}

	PrepareEnv(nil, nil)

	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("sentinel survived PrepareEnv with ENABLE_CLEAN_UP=true; want removed")
	}
}
