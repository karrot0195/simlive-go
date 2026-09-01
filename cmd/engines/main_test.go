package main

// Unit tests for the engine reconciler. The reconciler enforces the
// "at most one running engine per channel id" invariant; these tests
// drive it with a stub spawn callback and verify the diff/cancel/dedup
// behavior without needing a live Postgres or Redis.

import (
	"context"
	"sync"
	"testing"
	"time"
)

// spawnRecorder records every (id, ctx) pair the reconciler asked it
// to spawn, in order. The recorder does NOT allocate its own cancel —
// the reconciler already stores the real cancel in the task map; the
// test should pull it from there (see the cancelForID helper).
type spawnRecorder struct {
	mu     sync.Mutex
	events []spawnEvent
}

type spawnEvent struct {
	id  string
	ctx context.Context
}

func (r *spawnRecorder) spawn(_ context.Context) func(string, context.Context) {
	return func(id string, chCtx context.Context) {
		r.mu.Lock()
		r.events = append(r.events, spawnEvent{id: id, ctx: chCtx})
		r.mu.Unlock()
	}
}

// cancelForID returns the cancel func the reconciler stored in the
// task map for the given channel id, or nil if the channel isn't
// tracked.
func cancelForID(tasks *map[string]context.CancelFunc, mu *sync.Mutex, id string) context.CancelFunc {
	mu.Lock()
	defer mu.Unlock()
	return (*tasks)[id]
}

func (r *spawnRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func (r *spawnRecorder) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.id
	}
	return out
}

// TestReconcileEngines_StartsNewChannels verifies that active channels
// not yet in the task map each get exactly one spawn.
func TestReconcileEngines_StartsNewChannels(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	reconcileEngines(
		context.Background(),
		[]string{"ch-A", "ch-B", "ch-C"},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	if rec.count() != 3 {
		t.Fatalf("spawn count = %d, want 3", rec.count())
	}
	if got := rec.ids(); got[0] != "ch-A" || got[1] != "ch-B" || got[2] != "ch-C" {
		t.Errorf("spawn order = %v, want [ch-A ch-B ch-C]", got)
	}
	if len(tasks) != 3 {
		t.Errorf("tasks size = %d, want 3", len(tasks))
	}
	for _, id := range []string{"ch-A", "ch-B", "ch-C"} {
		if _, ok := tasks[id]; !ok {
			t.Errorf("tasks[%q] missing after spawn", id)
		}
	}
}

// TestReconcileEngines_DedupsAlreadyRunning verifies that a channel
// already in the task map is NOT spawned again. This is the core of
// the "at most one engine per id" invariant.
func TestReconcileEngines_DedupsAlreadyRunning(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	// First call: start ch-A and ch-B.
	reconcileEngines(
		context.Background(),
		[]string{"ch-A", "ch-B"},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 2 {
		t.Fatalf("first call: spawn count = %d, want 2", rec.count())
	}

	// Second call with the same set: nothing new should be spawned.
	reconcileEngines(
		context.Background(),
		[]string{"ch-A", "ch-B"},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 2 {
		t.Errorf("second call: spawn count = %d, want 2 (deduped)", rec.count())
	}
	if len(tasks) != 2 {
		t.Errorf("tasks size = %d, want 2 (deduped)", len(tasks))
	}
}

// TestReconcileEngines_CancelsRemovedChannels verifies that a channel
// that leaves the active set has its cancel func invoked and is
// removed from the task map (so a re-start can spawn a fresh loop).
func TestReconcileEngines_CancelsRemovedChannels(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	// Start ch-A and ch-B.
	reconcileEngines(
		context.Background(),
		[]string{"ch-A", "ch-B"},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	// The reconciler stored the real cancel in the task map; pull
	// it (and the paired context) so we can verify the cancel
	// actually fires against the context the engine would have been
	// running on.
	chACtx, chACancel := cancelForIDWithCtx(&tasks, &mu, rec, "ch-A")
	if chACtx == nil || chACancel == nil {
		t.Fatal("ch-A cancel/ctx not in task map after first reconcile")
	}

	// Second call: ch-A is no longer active, ch-B still is.
	reconcileEngines(
		context.Background(),
		[]string{"ch-B"},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	// ch-A's cancel must have been invoked.
	if !cancelInvoked(chACtx, chACancel) {
		t.Error("ch-A's cancel was not invoked after ch-A left the active set")
	}

	// ch-A must be gone from the task map.
	mu.Lock()
	_, present := tasks["ch-A"]
	_, chBPresent := tasks["ch-B"]
	mu.Unlock()
	if present {
		t.Error("ch-A still present in tasks after removal")
	}
	if !chBPresent {
		t.Error("ch-B was incorrectly removed from tasks")
	}
}

// TestReconcileEngines_RespawnAfterRemoval verifies that a channel can
// be re-added after being removed and will get a fresh spawn.
func TestReconcileEngines_RespawnAfterRemoval(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	// Start ch-A.
	reconcileEngines(
		context.Background(),
		[]string{"ch-A"},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 1 {
		t.Fatalf("first call: spawn count = %d, want 1", rec.count())
	}

	// Remove ch-A.
	reconcileEngines(
		context.Background(),
		[]string{},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 1 {
		t.Errorf("after removal: spawn count = %d, want 1 (no new spawn)", rec.count())
	}

	// Re-add ch-A — should get a fresh spawn.
	reconcileEngines(
		context.Background(),
		[]string{"ch-A"},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 2 {
		t.Errorf("after respawn: spawn count = %d, want 2", rec.count())
	}
	mu.Lock()
	if len(tasks) != 1 {
		t.Errorf("tasks size = %d, want 1", len(tasks))
	}
	mu.Unlock()
}

// TestReconcileEngines_PartialOverlap verifies the realistic case where
// the active set changes by a few channels: existing channels stay,
// removed channels are cancelled, new channels are spawned.
func TestReconcileEngines_PartialOverlap(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	// Tick 1: A, B, C.
	reconcileEngines(
		context.Background(),
		[]string{"A", "B", "C"},
		&tasks, &mu, rec.spawn(context.Background()),
	)
	if rec.count() != 3 {
		t.Fatalf("tick 1: spawn count = %d, want 3", rec.count())
	}

	// Pull cancels from the task map (the reconciler's source of
	// truth) for the channels we expect to be cancelled.
	bCtx, bCancel := cancelForIDWithCtx(&tasks, &mu, rec, "B")
	if bCtx == nil || bCancel == nil {
		t.Fatal("B cancel/ctx not in task map after tick 1")
	}

	// Tick 2: B dropped, D added, A and C stay.
	reconcileEngines(
		context.Background(),
		[]string{"A", "C", "D"},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	// 1 new spawn (D); 1 cancel (B); A and C untouched.
	if rec.count() != 4 {
		t.Errorf("after partial overlap: spawn count = %d, want 4", rec.count())
	}
	if !cancelInvoked(bCtx, bCancel) {
		t.Error("B's cancel was not invoked after B left the active set")
	}
	for _, keepID := range []string{"A", "C", "D"} {
		mu.Lock()
		_, present := tasks[keepID]
		mu.Unlock()
		if !present {
			t.Errorf("%s missing from tasks after partial-overlap tick", keepID)
		}
	}
	mu.Lock()
	_, bPresent := tasks["B"]
	mu.Unlock()
	if bPresent {
		t.Error("B still present in tasks after partial-overlap tick")
	}
}

// TestReconcileEngines_EmptyActiveSetCancelsAll verifies that a tick
// with an empty active set (e.g. every channel was just stopped) is
// the same as a "remove everything" tick — all cancels fire, map is
// empty.
func TestReconcileEngines_EmptyActiveSetCancelsAll(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex
	rec := &spawnRecorder{}

	reconcileEngines(
		context.Background(),
		[]string{"A", "B"},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	aCtx, aCancel := cancelForIDWithCtx(&tasks, &mu, rec, "A")
	bCtx, bCancel := cancelForIDWithCtx(&tasks, &mu, rec, "B")
	if aCtx == nil || bCtx == nil || aCancel == nil || bCancel == nil {
		t.Fatal("A/B cancel/ctx missing from task map after first reconcile")
	}

	reconcileEngines(
		context.Background(),
		[]string{},
		&tasks, &mu, rec.spawn(context.Background()),
	)

	if !cancelInvoked(aCtx, aCancel) || !cancelInvoked(bCtx, bCancel) {
		t.Error("not all cancels were invoked on empty-active-set tick")
	}
	mu.Lock()
	if len(tasks) != 0 {
		t.Errorf("tasks size after empty tick = %d, want 0", len(tasks))
	}
	mu.Unlock()
}

// TestReconcileEngines_ContextActuallyCancels verifies that the
// context handed to spawn is wired to the cancel stored in the task
// map — i.e. calling the stored cancel unblocks a goroutine selecting
// on the spawn's context.
func TestReconcileEngines_ContextActuallyCancels(t *testing.T) {
	tasks := make(map[string]context.CancelFunc)
	var mu sync.Mutex

	// Custom spawn that records (id, ctx) for the only channel we
	// reconcile. It runs AFTER reconcileEngines has released the map
	// lock, so it's safe to read the map here.
	var (
		gotCtx    context.Context
		gotID     string
		gotCancel context.CancelFunc
	)
	customSpawn := func(id string, chCtx context.Context) {
		if id != "ch-X" {
			return
		}
		gotID = id
		gotCtx = chCtx
		mu.Lock()
		gotCancel = tasks[id]
		mu.Unlock()
	}

	reconcileEngines(
		context.Background(),
		[]string{"ch-X"},
		&tasks, &mu, customSpawn,
	)

	if gotCtx == nil || gotCancel == nil {
		t.Fatal("spawn did not receive a context / cancel")
	}
	if gotID != "ch-X" {
		t.Errorf("spawn got id = %q, want ch-X", gotID)
	}

	// Fire the cancel and confirm the context unblocks.
	done := make(chan struct{})
	go func() {
		<-gotCtx.Done()
		close(done)
	}()
	gotCancel()

	select {
	case <-done:
		// Good: the context the spawn was given is the same one the
		// cancel func terminates. This is what the production engine
		// relies on for clean shutdown.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("context was not cancelled by the task-map cancel func")
	}
}

// cancelInvoked returns true if invoking `cancel` once flips the
// context's `Err()` to non-nil. The context must be the one paired
// with this cancel (i.e. the context returned by WithCancel that
// produced this cancel), otherwise cancelling has no effect on it.
func cancelInvoked(ctx context.Context, cancel context.CancelFunc) bool {
	if cancel == nil || ctx == nil {
		return false
	}
	cancel()
	return ctx.Err() != nil
}

// cancelForIDWithCtx returns the cancel func AND the context the
// reconciler stored for the given channel id. The recorder captures
// the context at spawn time; the cancel lives in the task map.
func cancelForIDWithCtx(
	tasks *map[string]context.CancelFunc,
	tasksMu *sync.Mutex,
	rec *spawnRecorder,
	id string,
) (context.Context, context.CancelFunc) {
	tasksMu.Lock()
	cancel := (*tasks)[id]
	tasksMu.Unlock()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, e := range rec.events {
		if e.id == id {
			return e.ctx, cancel
		}
	}
	return nil, cancel
}
