---
TYPE: REQUEST
DATE: 25-10-2026
VERSION: 1.0
LAST UPDATE: 2026-10-25
STATUS: done
SLUG: shared-data-cross-channel
SPEC_DIR: specs/002-shared-data-cross-channel/
SKILL_DIR: .agents/skills/shared-data-cross-channel-verify/
BRANCH: bugfix/release-v1-implement-version-number
---

LOGS:
- [2026-10-25 00:00] intake: request captured from partner feedback.
- [2026-10-25 00:00] specify: spec approved.
- [2026-10-25 00:00] clarify: Q1 resolved (no other shared state).
- [2026-10-25 00:00] plan: plan approved.
- [2026-10-25 00:00] tasks: tasks approved.
- [2026-10-25 00:00] implement: scoped shared state moved to SegmentProcessor.
- [2026-10-25 00:00] analyze: no critical findings.
- [2026-10-25 00:00] review: approved.
- [2026-10-25 00:00] verify: PASS.
---
RAW REQUEST:

> partner saild: if start simlive for 2 channels , data of channel 2 can be into channel 1 . It look happen when ads injected is finish and next video become wrong -> i guess some where use the same shared data

---

SUMMARY

**Issue Summary**

When running simlive for two channels concurrently, data belonging to channel 2 can leak into channel 1. The symptom appears around ad injection: after ads finish, the next video becomes wrong. Suspected cause is shared mutable state reused across channels (e.g. in-memory segment buffers / counters) instead of being scoped per channel.

**Impact**

Partners running multiple channels see wrong content after ad breaks on one channel, breaking playout correctness. Affects planner and possibly engine when multiple channels are active.

**What We Need**

1. Identify shared mutable state across channels (planner: processingSegments, isBlockingProcessing, adsId; any engine globals).
2. Scope all per-channel/per-process mutable state to the channel/process instance.
3. Verify no cross-channel contamination under 2-channel concurrent run.

---

EXPECTED DONE (Acceptance Criteria)

- [x] AC-001: Two channels running concurrently no longer mix segments; channel 1 playlist only contains channel 1 segments after ad breaks.
- [x] AC-002: Planner mutable state (processingSegments, isBlockingProcessing, adsId) is scoped per SegmentProcessor instance, not shared on Planner.
- [x] AC-003: Engine mutable state (inFlight, downloadingChannels, rendMetaCache) remains keyed by channelID; no global mutable buffers shared across channels.
- [x] AC-004: go build ./... passes.
- [x] AC-005: Verification report verdict PASS.

---

OPEN QUESTIONS

- Q1: Are there other shared mutable fields beyond planner (e.g. in engine or utils) causing cross-channel leak? → A: No; only planner held shared mutable state (processingSegments, isBlockingProcessing, adsId); engine state already keyed by channelID; utils has no globals.

---

FLOW (CURRENT_FLOW: done)

  speckit-specify (GATE)
    -> speckit-clarify (OPTIONAL)
    -> speckit-plan (GATE)
    -> speckit-tasks (GATE)
    -> speckit-implement
    -> speckit-analyze (GATE)
    -> speckit-review (GATE)
    -> verify (GATE: compare all vs request)

---

LINKS

- Request: requests/251026-partner-feedback-shared-data-cross-channel.md
- Spec: specs/002-shared-data-cross-channel/spec.md
- Plan: specs/002-shared-data-cross-channel/plan.md
- Tasks: specs/002-shared-data-cross-channel/tasks.md
- Verification Skill: .agents/skills/shared-data-cross-channel-verify/SKILL.md
- Verification Report: specs/002-shared-data-cross-channel/verification-report.md

---

VERIFICATION

Run two channels concurrently; inspect playlists and logs; ensure no cross-channel segment IDs.

---

NOTES

- STATUS values: intake | specify | clarify | plan | tasks | implement | analyze | review | verify | done
- A request is `done` only when the verification report verdict is PASS and all AC checkboxes are checked.
- Every status transition appends a LOGS entry (timestamp + change).
- OPEN QUESTIONS must all be answered (no PENDING) before leaving the clarify step.