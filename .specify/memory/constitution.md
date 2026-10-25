<!--
Sync Impact Report
Version change: 0.0.0 → 1.0.0
Modified principles: All 5 placeholders replaced with project-specific principles
Added sections: Architecture Constraints, Development Workflow, Governance
Removed sections: None (template placeholders only)
Follow-up TODOs: RATIFICATION_DATE needs historical confirmation
-->

# SimLive Constitution

## Core Principles

### I. Schema-First Knowledge Management
Every knowledge addition begins with a taxonomy proposal. The `projects/simlive/knowledge/` subtree follows the Karpathy LLM-wiki pattern: a `schema.md` defines the taxonomy tree (parseable by `lib.memory.taxonomy.parse_taxonomy`), hub pages (`<branch>/<branch>.md`) summarize and link children, and leaf pages carry `Parent: [[<hub>]]` and `## Related` cross-references. No leaf page is created without its hub existing first. The map (`projects/simlive/map.md`) points only at hubs, never at leaves.

### II. Data-Plane vs Instruction-Plane Separation
Project-specific durable knowledge lives under `projects/simlive/knowledge/` (data plane, `writer="llm-auto"`, quarantine-marked until human release). General practice (decisions, patterns, research) lives at root level and is promotion-gated. The `CLAUDE.md` marker block in the repo root is the only write into the scanned project; it is additive, idempotent, and preserves content outside `ren:` markers byte-for-byte.

### III. Streaming-First Architecture
SimLive is a simulated-live HLS streaming system. All components serve the playout loop: the planner builds EPG-driven timelines with SCTE-35 ad markers; the engine downloads, prepares, and rewrites PTS offsets for continuous DTS across VOD + ads; the API serves master/per-rendition playlists and segments. Changes to the playout pipeline (timeline model, segment preparation, PTS logic, rendition ladder) require integration tests against a running planner+engine+Redis+Postgres stack.

### IV. Observability & Structured Logging
All services use the shared `utils.Logger` with levelled output (VInfo, VWarning, VFatalf, Error). Structured logging with key-value pairs is mandatory for operational visibility. The planner and engine emit per-channel loggers (`PLANNER:{channel_id}`, `ENGINE:{channel_id}`) to correlate timeline processing with HLS delivery. Metrics (timeline size, cursor position, failure counters) are exposed via Redis keys and logged on every tick.

### V. Simplicity & YAGNI
Start simple. The rendition ladder is fixed (360p, 720p, 1080p). The segment cache is on-disk with Redis as the hot path. Ad segments are pre-loaded from a single ads bucket. No dynamic transcoding, no ABR switching logic in the engine — the player selects the rung. Complexity must be justified by a failing test or a documented operational need.

## Architecture Constraints

- **Language**: Go 1.26+ for all backend services; TypeScript/Vue 3 for the client.
- **Dependencies**: Gin (HTTP), Gorm/Postgres (persistence), Redis (timelines + HLS cache), rclone (object storage), SCTE35-Go (ad markers). New dependencies require a justification in the PR.
- **Deployment**: Docker Compose (Postgres 15, Redis 7) for local; makefile targets for service orchestration (`start-api`, `start-planner`, `start-engines`, `start-all`).
- **Configuration**: Environment variables via `godotenv`; settings struct in `internal/configs/configs.go`. No config files in the repo.
- **Time handling**: All timestamps in channel-local timezone; day boundaries via `StartOfDay`/`EndOfDay` in `internal/utils/times.go`.

## Development Workflow

- **Branching**: Feature branches from `main`; tags for releases (`release-v1`, etc.).
- **Testing**: Unit tests for pure logic (PTS math, m3u8 parsing, segment item construction). Integration tests for planner→engine→API flow require the full stack.
- **Code review**: All PRs must verify constitution compliance (schema-first, data-plane separation, streaming-first). Complexity must be justified.
- **Documentation**: Architecture decisions in `wiki/arch/`; task specs in `wiki/tasks/`; workstream logs in `wiki/workstreams/`. The `projects/simlive/` wiki is the living reference — update it when behavior changes.
- **Partner Request Flow**: Partner/raw requests follow the standardized flow in `.agents/skills/feature-delivery-flow/SKILL.md`. Intake format: `.specify/templates/request-template.md`. Each request gets a verification skill under `.agents/skills/<slug>-verify/`. Status tracked in the request file; resume via `CURRENT_FLOW`. A request is `done` only when its verification report verdict is PASS.

## Governance

This constitution supersedes all other practices for the SimLive project. Amendments require:
1. A proposal documenting the change and rationale.
2. Human approval (the "one human gate" — taxonomy changes require explicit confirmation before any write).
3. Version bump per semantic versioning: MAJOR for governance/principle removals or redefinitions; MINOR for new principles or materially expanded guidance; PATCH for clarifications.
4. Migration plan if existing wiki pages or code patterns are affected.

All PRs and reviews must verify compliance with this constitution. Use `projects/simlive/schema.md` as the source of truth for knowledge structure. Use `CLAUDE.md` marker block for repo↔wiki binding.

**Version**: 1.0.0 | **Ratified**: TODO(RATIFICATION_DATE): historical adoption date unknown | **Last Amended**: 2026-10-23