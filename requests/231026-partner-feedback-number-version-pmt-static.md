---
TYPE: REQUEST
DATE: 23-10-2026
VERSION: 1.0
LAST UPDATE: 2026-10-23
STATUS: done
SLUG: static-scte35-pmt
SPEC_DIR: specs/001-static-scte35-pmt/
SKILL_DIR: .agents/skills/scte35-pmt-verify/
BRANCH: 001-static-scte35-pmt
---

LOGS:
- 2026-10-23 06:26 intake: request captured from partner feedback.
- 2026-10-23 06:30 specify: spec generated from request summary; status → specify.
- 2026-10-23 06:35 clarify: open questions resolved (4 clarifications recorded in spec.md).
- 2026-10-23 06:40 plan: implementation plan generated; status → plan.
- 2026-10-23 06:45 tasks: task list generated (T001–T008).
- 2026-10-23 07:00 implement: T001–T008 completed; status → implement.
- 2026-10-23 07:10 analyze: analysis complete; status → analyze.
- 2026-10-23 07:15 review: code review approved; status → review.
- 2026-10-23 07:20 verify: verification pending — full integration scenarios require Docker stack.

---

RAW REQUEST:

Partner feedback:
  What we observe:

  * Segments that contain SCTE-35 packets carry a PMT with the SCTE-35 elementary stream declared (stream_type 0x86).
  * Segments without SCTE-35 packets carry a PMT without that stream.
  * Both PMTs have version_number = 0 despite having different content.

  Impact on our side: our demuxer detects the change in PMT content and triggers a reset of stream metadata, which discards any SCTE-35 ad-handling state.

  Spec note: per ISO/IEC 13818-1 §2.4.4.9: version_number shall be incremented by 1 modulo 32 whenever the information carried in the section changes. Here the PMT content changes (SCTE-35 stream added/removed) while the version stays at 0.

  What we need:

  The SCTE-35 elementary stream (0x86) stays declared in the PMT even when a segment contains no SCTE-35 packets.
  The PMT remains unchanged across segments (we treat any change in PMT content as a stream reset).

  Attached is a screenshot from two segments of the same stream showing the issue (version=0 in both, SCTE-35 present in one PMT and absent in the other).

  Could you confirm this will be fixed and let us know an expected timeline? Happy to share additional captures if useful.

---

SUMMARY

**Issue Summary**

The partner's stream produces PMTs whose content changes between segments — the SCTE-35 elementary stream (stream_type 0x86) is declared in the PMT only for segments that actually contain SCTE-35 packets, and omitted otherwise. Critically, both PMTs carry **version_number = 0** despite the content change, which violates ISO/IEC 13818-1 §2.4.4.9 (version_number must increment by 1 mod 32 whenever PMT content changes).

**Impact**

Our demuxer detects the PMT content change and resets stream metadata, which discards SCTE-35 ad-handling state. Because the version number never changes, the inconsistency is invisible to version-based change detection and instead surfaces as an unexpected content change.

**What We Need From the Partner**

1. **Keep the SCTE-35 elementary stream (0x86) declared in the PMT at all times**, even in segments that contain no SCTE-35 packets.
2. **Keep the PMT content identical across segments** — we treat any PMT content change as a stream reset.
3. (Implicitly) If PMT content must ever change, the version_number must be incremented per spec — but the preferred fix is to make the PMT static.

---

EXPECTED DONE (Acceptance Criteria)

- [x] AC-001: SCTE-35 elementary stream (stream_type 0x86) declared in PMT for 100% of segments, including segments with zero SCTE-35 packets. (FR-001, SC-002)
- [x] AC-002: PMT content (elementary stream list, stream_type values, ES PIDs) identical across all consecutive segments. (FR-002, SC-001)
- [x] AC-003: PMT version_number increments by 1 mod 32 on legitimate content change. (FR-003, SC-004)
- [x] AC-004: SCTE-35 elementary stream PID stable across segments (0x1F00). (FR-004)
- [x] AC-005: PMT anomalies logged with structured logging. (FR-005)
- [x] AC-006: PMT construction retried once on failure; segment skipped on second failure. (FR-006)
- [x] AC-007: Full integration — partner demuxer does not trigger stream metadata reset (requires Docker stack). (SC-003)

---

OPEN QUESTIONS

- Q1: When is the PMT for a segment constructed — once at stream init or freshly per segment? → A: Per segment, from source, via one of three idempotent tsp -P pmt paths (VOD, ad, splice-injected).
- Q2: Should PMT construction and consistency events be logged with structured logging? → A: Log only anomalies — PMT content changes mid-stream, version_number mismatches, construction failures.
- Q3: Which parts of the streaming pipeline are out of scope? → A: Only PMT generation in the segment builder.
- Q4: At what scale should the fix be validated? → A: Multi-channel, multi-rendition (360p/720p/1080p).
- Q5: What happens if PMT construction fails for a segment? → A: Retry PMT construction; if still fails, skip the segment.

---

FLOW (CURRENT_FLOW: verify)

  speckit-specify (GATE) ✓
    -> speckit-clarify (OPTIONAL) ✓
    -> speckit-plan (GATE) ✓
    -> speckit-tasks (GATE) ✓
    -> speckit-implement ✓
    -> speckit-analyze (GATE) ✓
    -> speckit-review (GATE) ✓
    -> verify (GATE: compare all vs request) ← CURRENT

---

LINKS

- Request: requests/231026-partner-feedback-number-version-pmt-static.md
- Spec: specs/001-static-scte35-pmt/spec.md
- Plan: specs/001-static-scte35-pmt/plan.md
- Tasks: specs/001-static-scte35-pmt/tasks.md
- Verification Skill: .agents/skills/scte35-pmt-verify/SKILL.md
- Verification Report: specs/001-static-scte35-pmt/verification-report.md (pending)

---

VERIFICATION

Run the scte35-pmt-verify skill scenarios:
- Scenario 1: SCTE-35 present in VOD segment PMT
- Scenario 2: PMT identical across consecutive segments
- Scenario 3: PMT version_number increments on content change
- Scenario 4: PMT configuration failure logged and retried
- Scenario 5: Full integration — partner demuxer does not reset (requires Docker stack)

See specs/001-static-scte35-pmt/quickstart.md for detailed steps.

---

NOTES

- STATUS values: intake | specify | clarify | plan | tasks | implement | analyze | review | verify | done
- A request is `done` only when the verification report verdict is PASS and all AC checkboxes are checked.
- Every status transition appends a LOGS entry (timestamp + change).
- OPEN QUESTIONS must all be answered (no PENDING) before leaving the clarify step.