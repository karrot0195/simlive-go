---
TYPE: REQUEST
DATE: [DD-MM-YYYY]
VERSION: 1.0
LAST UPDATE: [YYYY-MM-DD]
STATUS: intake
SLUG: [feature-slug]
SPEC_DIR: specs/[###-feature-name]/
SKILL_DIR: .agents/skills/[slug]-verify/
BRANCH: [partner/feature-slug or existing feature branch]
---

LOGS:
- [YYYY-MM-DD HH:MM] intake: request captured from partner feedback.

---

RAW REQUEST:

> (paste the partner's / raw request verbatim below)

---

SUMMARY

**Issue Summary**

[One paragraph describing the observed problem.]

**Impact**

[Who is affected and how.]

**What We Need**

1. [Requirement 1]
2. [Requirement 2]

---

EXPECTED DONE (Acceptance Criteria)

- [ ] AC-001: [measurable criterion 1]
- [ ] AC-002: [measurable criterion 2]

---

OPEN QUESTIONS

- Q1: [question] → A: PENDING

---

FLOW (CURRENT_FLOW: intake)

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

- Request: requests/[this-file].md
- Spec: [SPEC_DIR]spec.md
- Plan: [SPEC_DIR]plan.md
- Tasks: [SPEC_DIR]tasks.md
- Verification Skill: [SKILL_DIR]SKILL.md
- Verification Report: [SPEC_DIR]verification-report.md

---

VERIFICATION

[How to verify all ACs pass — commands + expected output.]

---

NOTES

- STATUS values: intake | specify | clarify | plan | tasks | implement | analyze | review | verify | done
- A request is `done` only when the verification report verdict is PASS and all AC checkboxes are checked.
- Every status transition appends a LOGS entry (timestamp + change).
- OPEN QUESTIONS must all be answered (no PENDING) before leaving the clarify step.
