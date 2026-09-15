---
name: sdlc-arch
description: 'Generate architecture documentation for simlive-go: detach tech stack, apply RULE, produce io/output.md (standard format) and wiki/arch/02-arch.md. Triggers: sdlc-arch, architecture, tech stack, detach, RULE.'
argument-hint: 'sdlc-arch [--scope all|tech-stack|rule]'
user-invocable: true
disable-model-invocation: false
---

# SDLC Architecture Skill (`sdlc-arch`)

## Purpose
Produce a reproducible architecture artifact set for the `simlive-go` simulated-live HLS playout system. Every run reads from `io/output.md` (standard format) and regenerates `wiki/arch/02-arch.md`.

## When to use
- User says: "sdlc-arch", "detach tech stack", "what RULE should apply", "generate architecture doc", "update 02-arch.md".
- Triggers: `sdlc-arch`, `architecture`, `tech stack`, `detach`, `RULE`.

## Inputs (from workspace)
- `go.mod` — Go 1.26.6, module `simlive`, dependencies (`gin`, `gorm`, `redis`, `scte35-go`, `rclone` wrapper).
- `docs/specification.md` — canonical spec (planner, engine, API, storage, quality tiers, SCTE-35, M1.5 ABR ladder).
- `.github/instructions/simlive-summary.instructions.md` — architecture snapshot (per-rung ZSETs, master playlist, bounded window, warm start, breaker counter).
- `docs/structure.md` — directory layout (`cmd/`, `internal/`, `config/`, `client/`).
- `client/package.json` — Vue 3 + Vite + Vuetify + `video.js` + `m3u8-parser`.
- `docker-compose.yml` — service orchestration.

## Procedure (step-by-step)

1. **Scan workspace** — read `go.mod`, `README.md`, `docs/structure.md`, `docs/specification.md` (first 200 lines), `.github/instructions/simlive-summary.instructions.md`, `client/package.json`, `docker-compose.yml`.
2. **Detach tech stack** — list language, framework, runtime, storage, messaging, media pipeline, client framework, build tools.
3. **Identify RULE** — the architecture rule that applies: every module (planner, engine, api, storage, config) must reference the same `docs/specification.md` as canonical; `io/output.md` is the machine-readable contract; `wiki/arch/02-arch.md` is the human-readable architecture doc; both must stay in sync with `.github/instructions/simlive-summary.instructions.md`.
4. **Read `io/output.md`** (standard format) — canonical input/output contract.
5. **Generate / update outputs:**
   - `.github/skills/sdlc-arch/io/output.md` — standard format (tech_stack, rule, file references, generation timestamp).
   - `wiki/arch/02-arch.md` — architecture doc (must reference `io/output.md` and `01-summary.md`).
6. **Reconcile** — verify `02-arch.md` links to `output.md`; verify `output.md` reflects latest scan.

## Standard format (`io/output.md`)
Every run produces / updates this file. It must contain:
- `project_name`
- `language`
- `domain`
- `tech_stack` (list: backend language + version, framework, DB, cache, media lib, storage wrapper, client framework, build tool)
- `rule` (the architecture RULE — title + description)
- `rule_applies_to` (list of modules / file patterns)
- `feature_doc_path` (relative path, e.g. `wiki/arch/02-arch.md`)
- `summary_doc_path` (e.g. `wiki/arch/01-summary.md`)
- `generated_at` (ISO timestamp)
- `source_refs` (list of files read during scan)

## Quality criteria / completion checks
- [ ] `.github/skills/sdlc-arch/io/` folder exists.
- [ ] `.github/skills/sdlc-arch/io/output.md` exists and follows standard format.
- [ ] `wiki/arch/02-arch.md` exists and references `io/output.md`.
- [ ] `tech_stack` is detached (each layer named with version/reference).
- [ ] `rule` is stated explicitly and applies to all modules.
- [ ] Every output file is consistent with `docs/specification.md` + `.github/instructions/simlive-summary.instructions.md`.

## Related skills
- `sdlc-ingest-project` — for full project scan (`io/out.md`, `01-summary.md`, feature docs).
- `simlive-plan` — for feature plan files (`docs/ddMMyy_HHmm-*.md`).
- `simlive-doc-pass` — for post-task documentation reconciliation (`docs/memories.md`, spec, instructions).
