---
name: sdlc-ingest-project
description: 'Scan current project, summarize what it is / support / feature, generate standard output files (io/out.md, wiki/arch/01-summary.md, feature detail docs). Every run reads from io/out.md and generates into it.'
argument-hint: 'sdlc-ingest-project [--scope all|summary|feature]'
user-invocable: true
disable-model-invocation: false
---

# SDLC Ingest Project Skill

## Purpose
Ingest a Go/HLS playout project (`simlive-go`) and produce a standard, reproducible documentation set that every future run of this skill reads from and regenerates.

## When to use
- User says: "scan current project", "summary what is it", "what is support / feature", "create io folder inside skill", "everyone run this skill will read from it and generate".
- Triggers: `sdlc-ingest-project`, `ingest`, `project scan`, `summary`, `feature detail`.

## Inputs (from prompt / workspace)
- Workspace root: `/home/kai/Workspaces/espx/simlive-project/simlive-go`
- Key source files: `docs/specification.md`, `.github/instructions/simlive-summary.instructions.md`, `docs/memories.md`, `docs/structure.md`, `README.md`, `go.mod`
- Feature reference: planner prepare mapping data (`docs/300826_0620-multi-rendition-abr-ladder.md`, planner module `internal/planner/planner.go`)

## Procedure (step-by-step)

1. **Scan workspace** — list root, read `README.md`, `go.mod`, `docs/structure.md`, `docs/specification.md` (first 200 lines), `.github/instructions/simlive-summary.instructions.md`.
2. **Identify project identity** — name (`simlive-go`), language (`Go`), domain (`simulated-live HLS playout`), key modules (`planner`, `engine`, `api`, `models`, `storage`).
3. **Identify support** — what the system supports: planner (EPG → Redis timeline), engine (sliding HLS playlist + PTS rewrite), API (channel CRUD + HLS routes), storage (`rclone`), quality tiers (`360p/720p/1080p`), SCTE-35 cue markers, multi-rendition ABR ladder (M1.5 shared cache + 3 advertised rungs).
4. **Identify feature** — the primary feature to document in detail: planner prepare mapping data (`prepareDate`, `ProcessEPGToTimeline`, `SegmentProcessor`, `resolveRenditionsFor`, `writeRenditionMetadata`, `simlive:timeline:{ch}:{rend}` ZSET, `simlive:renditions:{ch}` metadata, legacy-timeline flush, warm-pickup loop).
5. **Read `io/out.md`** (standard format) — this is the canonical input/output contract. Every run reads it first, then writes updates back.
6. **Generate / update outputs:**
   - `io/out.md` — standard format (project identity, support, feature, file references, generation timestamp).
   - `wiki/arch/01-summary.md` — high-level summary (must reference feature docs).
   - `wiki/arch/feature/01-planner-prepare-mapping-data.md` — detailed feature doc (must appear in `01-summary.md`).
7. **Reconcile** — verify `01-summary.md` links to the feature doc; verify `io/out.md` reflects the latest scan.

## Standard format (`io/out.md`)
Every run produces / updates this file. It must contain:
- `project_name`
- `language`
- `domain`
- `modules` (list)
- `support` (what the system supports — bullet list)
- `feature` (primary feature being detailed — title + file reference)
- `feature_doc_path` (relative path, e.g. `wiki/arch/feature/01-planner-prepare-mapping-data.md`)
- `summary_doc_path` (e.g. `wiki/arch/01-summary.md`)
- `generated_at` (ISO timestamp)
- `source_refs` (list of files read during scan)

## Quality criteria / completion checks
- [ ] `io/` folder exists inside skill (`.github/skills/sdlc-ingest-project/io/`).
- [ ] `io/out.md` exists and follows standard format.
- [ ] `wiki/arch/01-summary.md` exists and references the feature doc.
- [ ] `wiki/arch/feature/01-planner-prepare-mapping-data.md` exists and describes planner prepare mapping data in detail.
- [ ] Every output file is readable and consistent with `docs/specification.md` + `.github/instructions/simlive-summary.instructions.md`.

## Related skills
- `simlive-plan` — for generating feature plan files (`docs/ddMMyy_HHmm-*.md`).
- `simlive-doc-pass` — for post-task documentation reconciliation (`docs/memories.md`, spec, instructions).
