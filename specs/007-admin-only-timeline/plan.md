# Implementation Plan: Admin-Only Timeline Screen

**Branch**: `007-admin-only-timeline` | **Date**: 2026-10-29 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/007-admin-only-timeline/spec.md`

## Summary

Restrict the timeline screen to admin users whose email matches `VITE_ADMIN_ID`. Hide the timeline entry point for non-admins and block direct route access.

## Technical Context

**Language/Version**: TypeScript (Vue 3)
**Primary Dependencies**: Vue 3, Vite, Pinia, Vue Router
**Storage**: N/A (client-side gate)
**Testing**: Manual UI verification
**Target Platform**: Web browser
**Project Type**: Web application (frontend)
**Performance Goals**: N/A
**Constraints**: No backend changes; env-driven admin email
**Scale/Scope**: Single admin email value

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **Streaming-First (III)**: Not applicable (client UI). PASS.
- **Observability (IV)**: N/A. PASS.
- **Simplicity & YAGNI (V)**: Env-driven single value, no new dependencies. PASS.

## Project Structure

### Documentation (this feature)

```text
specs/007-admin-only-timeline/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
└── tasks.md
```

### Source Code (repository root)

```text
client/src/
├── router/index.ts
├── stores/auth.ts
└── views/Panel.vue (or navigation component)
```

**Structure Decision**: Frontend-only changes in `client/src`.

## Complexity Tracking

No constitution violations. No complexity tracking required.