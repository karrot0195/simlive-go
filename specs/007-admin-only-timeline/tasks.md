# Tasks: Admin-Only Timeline Screen

**Input**: Design documents from `/specs/007-admin-only-timeline/`

**Prerequisites**: plan.md (required), spec.md (required for user stories)

**Tests**: Optional; manual verification.

**Organization**: Tasks grouped by user story.

## Phase 1: Setup

(No setup tasks required.)

## Phase 2: Foundational

(No foundational tasks required.)

## Phase 3: User Story 1 - Admin can access timeline (Priority: P1)

**Goal**: Admin users see and can access the timeline.

**Independent Test**: Log in with admin email; verify link visible and route loads.

- [x] T001 [US1] In `client/src/router/index.ts`, add a route guard for `/timeline` allowing access only when the current user's email (case-insensitive) matches `import.meta.env.VITE_ADMIN_ID`.
- [x] T002 [P] [US1] In the navigation component (e.g., `client/src/views/Panel.vue`), conditionally render the timeline link only when the user is admin.

## Phase 4: User Story 2 - Non-admin cannot see or access timeline (Priority: P1)

**Goal**: Non-admins cannot see or access the timeline.

**Independent Test**: Log in with non-admin email; verify link hidden and direct access blocked.

- [x] T003 [US2] In `client/src/router/index.ts`, redirect non-admin direct `/timeline` access to the home route (or show a denied state).
- [x] T004 [P] [US2] Ensure `VITE_ADMIN_ID` empty/unset results in no admin (deny all).

## Final Phase: Polish & Cross-Cutting Concerns

- [x] T005 Document `VITE_ADMIN_ID` in client env example/config.

## Dependencies & Execution Order

- T001/T002 can run in parallel.
- T003 depends on T001.
- T004/T005 can run in parallel.