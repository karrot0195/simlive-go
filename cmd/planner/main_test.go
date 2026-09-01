package main

// Unit tests for the planner per-tick reconciler.
//
// The reconciler enforces "at most one in-flight worker per channel id
// at any time" so ProcessEPGToTimeline is never called concurrently
// for the same channel (which is unsafe per the NOTE at
// internal/planner/planner.go NewPlanner). These tests drive
// runPlannerTick with a stub process callback and assert the
// in-flight semantics without needing a live Postgres or Redis.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"simlive/internal/models"
)

// TestRunPlannerTick_RunsEachChannelOnce verifies the basic happy
// path: every active channel is processed exactly once.
func TestRunPlannerTick_RunsEachChannelOnce(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	processCalls := make(chan string, 8)
	process := func(ch models.PlayoutChannel) {
		processCalls <- ch.ID
	}

	channels := []models.PlayoutChannel{
		{ID: "A", Name: "Alpha"},
		{ID: "B", Name: "Bravo"},
		{ID: "C", Name: "Charlie"},
	}
	runPlannerTick(channels, 4, &inFlightMu, inFlight, process)

	if got := len(processCalls); got != 3 {
		t.Fatalf("process call count = %d, want 3", got)
	}
	close(processCalls)
	got := make(map[string]bool)
	for id := range processCalls {
		got[id] = true
	}
	for _, id := range []string{"A", "B", "C"} {
		if !got[id] {
			t.Errorf("process was not called for channel %q", id)
		}
	}
}

// TestRunPlannerTick_ReleasesInFlightOnCompletion verifies that the
// in-flight slot is released after the worker finishes, so a
// subsequent tick can claim it.
func TestRunPlannerTick_ReleasesInFlightOnCompletion(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	process := func(ch models.PlayoutChannel) {
		// Empty body — just exercise the bookkeeping.
	}

	runPlannerTick([]models.PlayoutChannel{{ID: "A"}}, 4, &inFlightMu, inFlight, process)

	inFlightMu.Lock()
	defer inFlightMu.Unlock()
	if _, busy := inFlight["A"]; busy {
		t.Error("inFlight[\"A\"] was not released after the worker completed")
	}
}

// TestRunPlannerTick_SkipsChannelAlreadyInFlight is the core
// concurrency test. A slow process on tick 1 holds the in-flight
// slot; a tick 2 (run in a goroutine) for the same channel must
// skip and not call process.
//
// We use a start barrier (`started`) so the test can confirm tick 1
// is actually executing process before launching tick 2, and a
// release channel (`release`) to let tick 1 finish once the test has
// verified tick 2 skipped.
func TestRunPlannerTick_SkipsChannelAlreadyInFlight(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	started := make(chan struct{}, 1) // process() sends here when entered
	release := make(chan struct{})    // closed to let tick 1's process() return

	var firstStarted, secondStarted atomic.Bool

	// Only the first invocation (tick 1) blocks on `release` after
	// signaling `started`; any second invocation (which should NOT
	// happen — the in-flight guard should skip tick 2) records itself
	// via secondStarted so the test can fail loudly.
	process := func(ch models.PlayoutChannel) {
		if firstStarted.CompareAndSwap(false, true) {
			started <- struct{}{}
			<-release
			return
		}
		secondStarted.Store(true)
	}

	// Run tick 1 in its own goroutine.
	tick1Done := make(chan struct{})
	go func() {
		runPlannerTick([]models.PlayoutChannel{{ID: "A"}}, 1, &inFlightMu, inFlight, process)
		close(tick1Done)
	}()

	// Wait until tick 1 has actually entered process (not just
	// claimed the slot). This is the strongest guarantee that the
	// in-flight slot is held when tick 2 runs.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("tick 1 never entered process")
	}

	// Run tick 2 on the SAME in-flight registry while tick 1 is
	// still holding the slot. This call must complete quickly with
	// process NOT invoked.
	tick2Done := make(chan struct{})
	go func() {
		runPlannerTick([]models.PlayoutChannel{{ID: "A"}}, 1, &inFlightMu, inFlight, process)
		close(tick2Done)
	}()

	// Give tick 2's worker a moment to observe the busy slot and
	// bail. If it's still running after 500ms, the guard is broken.
	select {
	case <-tick2Done:
		// Good: tick 2 finished without invoking process.
	case <-time.After(500 * time.Millisecond):
		// Force-release tick 1 so the test doesn't hang on cleanup.
		close(release)
		<-tick1Done
		t.Fatal("tick 2 did not return; in-flight guard appears broken")
	}

	if secondStarted.Load() {
		// Allow tick 1 to finish.
		close(release)
		<-tick1Done
		t.Fatal("process was invoked on tick 2 while tick 1 was still in flight")
	}

	// Let tick 1 finish.
	close(release)
	<-tick1Done

	inFlightMu.Lock()
	_, stillBusy := inFlight["A"]
	inFlightMu.Unlock()
	if stillBusy {
		t.Error("inFlight[\"A\"] not released after tick 1 completed")
	}
}

// TestRunPlannerTick_RunsAgainAfterCompletion verifies the slot is
// reclaimed across ticks: once a worker is done, the NEXT tick can
// claim the slot and run process again.
func TestRunPlannerTick_RunsAgainAfterCompletion(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	var count atomic.Int32
	process := func(ch models.PlayoutChannel) {
		count.Add(1)
	}

	runPlannerTick([]models.PlayoutChannel{{ID: "A"}}, 4, &inFlightMu, inFlight, process)
	runPlannerTick([]models.PlayoutChannel{{ID: "A"}}, 4, &inFlightMu, inFlight, process)

	if got := count.Load(); got != 2 {
		t.Errorf("process call count = %d, want 2 (two sequential ticks)", got)
	}
}

// TestRunPlannerTick_RespectsWorkerCount verifies that the bounded
// worker pool actually bounds concurrency. We launch N processes
// each blocking on a barrier, then count the number that are
// simultaneously in `process`.
func TestRunPlannerTick_RespectsWorkerCount(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	const n = 6
	const workers = 2

	entered := make(chan struct{}, n)
	release := make(chan struct{})

	process := func(ch models.PlayoutChannel) {
		entered <- struct{}{}
		<-release
	}

	channels := make([]models.PlayoutChannel, n)
	for i := 0; i < n; i++ {
		channels[i] = models.PlayoutChannel{ID: makeID(i)}
	}

	done := make(chan struct{})
	go func() {
		runPlannerTick(channels, workers, &inFlightMu, inFlight, process)
		close(done)
	}()

	// We expect exactly `workers` goroutines to enter process and
	// then block. We do NOT expect more.
	timeout := time.After(500 * time.Millisecond)
	count := 0
	for count < workers {
		select {
		case <-entered:
			count++
		case <-timeout:
			close(release)
			<-done
			t.Fatalf("timed out waiting for %d workers; got %d", workers, count)
		}
	}
	// The remaining (n - workers) workers should still be parked at
	// the semaphore. If more slipped through, we'll see it here.
	select {
	case <-entered:
		close(release)
		<-done
		t.Fatal("more than `workers` goroutines entered process; semaphore broken")
	case <-time.After(50 * time.Millisecond):
		// Good — no more than `workers` are running.
	}

	// Let everything finish.
	close(release)
	<-done
}

// TestRunPlannerTick_DifferentChannelsRunInParallel verifies the
// "at most one in flight per channel id" rule does NOT serialize
// across DIFFERENT channel ids — two different channels can run
// in parallel up to the worker pool size.
func TestRunPlannerTick_DifferentChannelsRunInParallel(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	release := make(chan struct{})
	process := func(ch models.PlayoutChannel) {
		<-release
	}

	done := make(chan struct{})
	go func() {
		runPlannerTick(
			[]models.PlayoutChannel{{ID: "A"}, {ID: "B"}},
			2,
			&inFlightMu, inFlight, process,
		)
		close(done)
	}()

	// Wait until both A and B are in flight.
	deadline := time.After(500 * time.Millisecond)
	for {
		inFlightMu.Lock()
		_, aBusy := inFlight["A"]
		_, bBusy := inFlight["B"]
		inFlightMu.Unlock()
		if aBusy && bBusy {
			break
		}
		select {
		case <-deadline:
			close(release)
			<-done
			t.Fatal("A and B did not run in parallel; the in-flight guard is over-broad")
		case <-time.After(10 * time.Millisecond):
			// Retry.
		}
	}

	close(release)
	<-done
}

// TestRunPlannerTick_EmptyActiveSet is a smoke test: a tick with no
// active channels completes immediately without invoking process.
func TestRunPlannerTick_EmptyActiveSet(t *testing.T) {
	var inFlightMu sync.Mutex
	inFlight := map[string]struct{}{}

	var called atomic.Bool
	process := func(ch models.PlayoutChannel) { called.Store(true) }

	runPlannerTick(nil, 4, &inFlightMu, inFlight, process)
	if called.Load() {
		t.Error("process was invoked on an empty active set")
	}
}

// --- helpers ------------------------------------------------------------

// makeID returns a deterministic short id for the i-th test channel.
// Not a UUID — just a stable string for table tests.
func makeID(i int) string {
	const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if i < 0 || i >= len(digits) {
		return "CH"
	}
	return "ch-" + string(digits[i])
}
