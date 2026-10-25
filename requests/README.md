# Requests Index

Table of all partner/raw requests tracked in this project.

| Date | Slug | Status | Spec Dir | Verification Skill | Request File |
|------|------|--------|----------|--------------------|--------------|
| 2026-10-23 | static-scte35-pmt | verify | specs/001-static-scte35-pmt/ | .agents/skills/scte35-pmt-verify/ | [231026-partner-feedback-number-version-pmt-static.md](231026-partner-feedback-number-version-pmt-static.md) |

## Status Values

`intake` | `specify` | `clarify` | `plan` | `tasks` | `implement` | `analyze` | `review` | `verify` | `done`

## Conventions

- Each request file follows `.specify/templates/request-template.md`.
- Each request has a verification skill under `.agents/skills/<slug>-verify/`.
- A request is `done` only when its verification report verdict is PASS and all AC checkboxes are checked.
- See `.agents/skills/feature-delivery-flow/SKILL.md` for the standardized flow.