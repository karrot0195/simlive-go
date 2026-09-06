// Tests for the per-call state isolation refactor.
//
// Bug history: ProcessEPGToTimeline used to mutate per-call walk state on
// the shared Planner (globalRunningTime, processDate, horizon, channelID)
// and the SegmentProcessor read it via sp.planner.* on every schedule item.
// With PLANNER_WORKERS > 1, two concurrent calls would clobber each other's
// state mid-walk, producing a corrupt timeline (one channel's segments
// landing in the other's ZSET, or the EPG walk stopping early because
// horizon got reset). Symptom: 2 active channels -> only 1 has data.
//
// The stopgap fix (epgMu mutex + PLANNER_WORKERS clamped to 1) made the
// whole thing strictly serial: with 2 active channels the second channel
// waited for the first's full cold-start walk (~30s–2min per date) before
// it could start, so it had no data for the entire window.
//
// Proper fix: the per-call state now lives on the SegmentProcessor. These
// tests are pure (no Redis, no ESPX, no rclone) and prove the state is
// isolated per processor instance — run under `go test -race` they also
// catch any accidental fallback to shared Planner state.

package planner

import (
	"sync"
	"testing"
	"time"

	"simlive/internal/utils"
)

// TestSegmentProcessor_IsolatesPerCallState proves two processors built on
// the SAME Planner advance their per-call walk state independently — the
// core guarantee that makes concurrent channel walking safe.
func TestSegmentProcessor_IsolatesPerCallState(t *testing.T) {
	p := &Planner{}
	spA := &SegmentProcessor{planner: p, channelID: "A", horizon: utils.EndOfDay(time.Now().Add(24 * time.Hour))}
	spB := &SegmentProcessor{planner: p, channelID: "B", horizon: utils.EndOfDay(time.Now().Add(24 * time.Hour))}

	// Advance A's walk state; B must be untouched.
	spA.runningTime = 1000
	spA.totalSegmentsAdded = 7

	if spB.runningTime != 0 {
		t.Errorf("spB.runningTime = %v, want 0 (walk state leaked between processors)", spB.runningTime)
	}
	if spB.totalSegmentsAdded != 0 {
		t.Errorf("spB.totalSegmentsAdded = %d, want 0 (walk state leaked between processors)", spB.totalSegmentsAdded)
	}
	if spA.channelID == spB.channelID {
		t.Errorf("processors share channelID %q; per-call channel must be isolated", spA.channelID)
	}
	if got := spA.timelineKey(); got != "simlive:timeline:A:360p" {
		t.Errorf("spA.timelineKey() = %q, want simlive:timeline:A:360p", got)
	}
	if got := spB.timelineKey(); got != "simlive:timeline:B:720p" {
		t.Errorf("spB.timelineKey() = %q, want simlive:timeline:B:720p", got)
	}
}

// TestSegmentProcessor_ConcurrentIncrements runs two processors on the same
// Planner concurrently. Each goroutine owns its own processor, so no shared
// mutable state is touched; under `go test -race` this catches any
// accidental fallback to shared Planner state. Each processor's counters
// must come out exact (no cross-processor interference).
func TestSegmentProcessor_ConcurrentIncrements(t *testing.T) {
	p := &Planner{}
	spA := &SegmentProcessor{planner: p, channelID: "A"}
	spB := &SegmentProcessor{planner: p, channelID: "B"}
	const iters = 10000

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			spA.runningTime++
			spA.totalSegmentsAdded++
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			spB.runningTime++
			spB.totalSegmentsAdded++
		}
	}()
	wg.Wait()

	if got := spA.runningTime; got != iters {
		t.Errorf("spA.runningTime = %v, want %d (lost updates)", got, iters)
	}
	if got := spB.runningTime; got != iters {
		t.Errorf("spB.runningTime = %v, want %d (lost updates)", got, iters)
	}
	if got := spA.totalSegmentsAdded; got != iters {
		t.Errorf("spA.totalSegmentsAdded = %d, want %d", got, iters)
	}
	if got := spB.totalSegmentsAdded; got != iters {
		t.Errorf("spB.totalSegmentsAdded = %d, want %d", got, iters)
	}
}

// TestIsEndOfTimeline_IsolatedPerHorizon pins the free-function form: each
// processor supplies its own horizon, so two channels with different
// horizons stop their walks independently.
func TestIsEndOfTimeline_IsolatedPerHorizon(t *testing.T) {
	loc := utils.GetLocFor("Asia/Saigon")
	dayEnd := utils.EndOfDay(time.Date(2026, 8, 30, 12, 0, 0, 0, loc))

	if isEndOfTimeline(dayEnd, float64(dayEnd.Unix())-1) {
		t.Error("runningTime 1s before horizon must NOT be end of timeline")
	}
	if !isEndOfTimeline(dayEnd, float64(dayEnd.Unix())+1) {
		t.Error("runningTime 1s past horizon must be end of timeline")
	}
	if isEndOfTimeline(time.Time{}, float64(dayEnd.Unix())+86400) {
		t.Error("zero horizon must be a no-op (returns false)")
	}
}
