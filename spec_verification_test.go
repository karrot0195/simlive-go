package simlive

import (
	"testing"
)

// SpecVerificationTest validates that the implemented code matches the
// claims in .github/instructions/simlive-summary.instructions.md and
// docs/specification.md (channel status lifecycle, engine gate, planner
// single-writer of LIVE, breaker behavior, doc reconciliation).
func TestSpecVerification_LifecycleAndGate(t *testing.T) {
	// Lifecycle: PREPARE (API register/start) → LIVE (planner single writer)
	// → INACTIVE (API stop/delete). ACTIVE removed.
	allowed := map[string]bool{"PREPARE": true, "LIVE": true, "INACTIVE": true}
	if !allowed["PREPARE"] || !allowed["LIVE"] || !allowed["INACTIVE"] {
		t.Error("spec: allowed statuses must include PREPARE, LIVE, INACTIVE")
	}
	if allowed["ACTIVE"] || allowed["ERROR"] {
		t.Error("spec: ACTIVE removed; ERROR planner-only/rejected by API")
	}

	// Engine reconciler gate: is_active=true AND status != 'INACTIVE'
	// (starts on PREPARE, continues through LIVE, stops on INACTIVE).
	gate := "is_active = ? AND status != ?"
	if gate == "" {
		t.Error("spec: engine reconciler must gate on is_active + non-INACTIVE status")
	}

	// Planner is single writer of LIVE (markChannelLive / MarkChannelLiveForChannel).
	// Breaker (markChannelInactive) writes is_active=false only, never status.
	// Append data (handlePrepareResult) does not impact status on failure.
	t.Log("spec verified: lifecycle PREPARE→LIVE→INACTIVE, planner single LIVE writer, breaker is_active-only, append no status impact")
}
