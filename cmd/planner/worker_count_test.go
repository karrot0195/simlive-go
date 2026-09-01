// Tests for plannerWorkerCount() in cmd/planner/main.go. Since the
// parallel-processing refactor moved per-call walk state onto the
// SegmentProcessor, different channels can be prepared concurrently and the
// default is back to 4 (the pre-epgMu value). Values < 1 are floored to 1.

package main

import "testing"

// TestPlannerWorkerCount_DefaultIsFour pins the default. ProcessEPGToTimeline
// is safe to call concurrently for different channels because per-call state
// lives on the SegmentProcessor; cmd/planner must default to parallel
// processing so a multi-channel fleet fills its timelines in one tick.
func TestPlannerWorkerCount_DefaultIsFour(t *testing.T) {
	t.Setenv("PLANNER_WORKERS", "")
	got := plannerWorkerCount()
	if got != 4 {
		t.Errorf("default = %d, want 4 (parallel channel processing)", got)
	}
}

// TestPlannerWorkerCount_HonorsEnvValue ensures an explicit
// PLANNER_WORKERS value is honoured (no clamp to 1 anymore).
func TestPlannerWorkerCount_HonorsEnvValue(t *testing.T) {
	cases := map[string]int{"1": 1, "4": 4, "8": 8}
	for env, want := range cases {
		t.Setenv("PLANNER_WORKERS", env)
		if got := plannerWorkerCount(); got != want {
			t.Errorf("env=%s -> got %d, want %d", env, got, want)
		}
	}
}

// TestPlannerWorkerCount_FloorsAtOne documents that zero/negative values are
// floored to 1 (a worker pool of 0 would deadlock the tick loop).
func TestPlannerWorkerCount_FloorsAtOne(t *testing.T) {
	for _, v := range []string{"0", "-3"} {
		t.Setenv("PLANNER_WORKERS", v)
		if got := plannerWorkerCount(); got != 1 {
			t.Errorf("env=%s -> got %d, want 1 (floor)", v, got)
		}
	}
}
