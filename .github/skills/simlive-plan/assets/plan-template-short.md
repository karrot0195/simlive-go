<!--
  Short plan template for trivial SimLive-Go changes.
  The scaffolder copies this file into ./docs/ddMMyy-<purpose>.md.
-->

# [TODO: Feature title]

**Date:** [TODO: YYYY-MM-DD]  
**Scope:** Short checklist (use full plan for non-trivial changes).

---

## 0. Progress

- [ ] Read pre-flight context (`docs/specification.md`, summary instructions).
- [ ] Drafted §1–§4 below.
- [ ] Implementation done.
- [ ] Verification (§3) passed.
- [ ] Post-task docs updated.

---

## 1. Problem

[TODO: 2-4 sentences.]

## 2. Steps

- [ ] **[TODO: file]** — [TODO: one-liner change].
- [ ] **[TODO: file]** — [TODO: one-liner change].
- [ ] **[TODO: file]** — [TODO: one-liner change].
- [ ] **Docs** — update `docs/specification.md` §[TODO] and the summary
      instructions file if models/env vars/architecture moved.

## 3. Verification

- [ ] `GOCACHE=$PWD/.gocache go build ./cmd/... ./internal/...` — clean.
- [ ] [TODO: log line or metric to confirm].
- [ ] [TODO: manual probe if playback-affecting].

## 4. Notes / Out of scope

[TODO: bullet list.]
