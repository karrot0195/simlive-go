# Feature Specification: Admin-Only Timeline Screen

**Feature Branch**: `007-admin-only-timeline`

**Created**: 2026-10-29

**Status**: Draft

**Input**: User description: "Hide special feature for admin only. Screen affected: timeline. Define env as VITE_ADMIN_ID=admin@gmail.com (changeable via env). If current user email equals VITE_ADMIN_ID, allow see and access; otherwise hide and deny."

## Clarifications

### Session 2026-10-29

- Q: Should the admin email comparison be case-sensitive or case-insensitive? → A: Case-insensitive comparison.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin can access timeline (Priority: P1)

An admin user (email matching `VITE_ADMIN_ID`) can see the timeline entry point and access the timeline screen.

**Why this priority**: Admins need the timeline for operational monitoring.

**Independent Test**: Log in with the admin email; verify the timeline link is visible and the route loads.

**Acceptance Scenarios**:

1. **Given** a logged-in user whose email equals `VITE_ADMIN_ID`, **When** the app renders navigation, **Then** the timeline entry point is visible.
2. **Given** the same admin user, **When** they navigate to the timeline route, **Then** the screen loads successfully.

### User Story 2 - Non-admin cannot see or access timeline (Priority: P1)

A non-admin user does not see the timeline entry point and cannot access the timeline route.

**Why this priority**: Prevents unauthorized access to the special feature.

**Independent Test**: Log in with a non-admin email; verify no timeline link and route access is denied.

**Acceptance Scenarios**:

1. **Given** a logged-in user whose email does not equal `VITE_ADMIN_ID`, **When** the app renders navigation, **Then** the timeline entry point is hidden.
2. **Given** the same non-admin user, **When** they attempt to open the timeline route directly, **Then** access is denied (redirect or blocked).

### Edge Cases

- What happens when `VITE_ADMIN_ID` is unset or empty? Treat no user as admin (deny all).
- What happens when user email is unavailable (not logged in)? Hide and deny.
- Email comparison is case-insensitive.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST read the admin email from environment variable `VITE_ADMIN_ID` (default `admin@gmail.com`).
- **FR-002**: System MUST allow timeline visibility and access only when the current user's email equals `VITE_ADMIN_ID`.
- **FR-003**: System MUST hide the timeline entry point from non-admin users.
- **FR-004**: System MUST deny direct navigation to the timeline route for non-admin users.
- **FR-005**: System MUST treat unset/empty `VITE_ADMIN_ID` as "no admin" (deny all).
- **FR-006**: System MUST treat unauthenticated users as non-admin (hide and deny).

### Key Entities *(include if feature involves data)*

- **Current user**: Authenticated user record containing an email.
- **Admin email**: Value from `VITE_ADMIN_ID`.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of non-admin users do not see the timeline entry point.
- **SC-002**: 100% of direct timeline route attempts by non-admins are blocked.
- **SC-003**: Admin users can access the timeline without additional steps.
- **SC-004**: Changing `VITE_ADMIN_ID` changes admin access without code changes.

## Assumptions

- Admin determination is based solely on email comparison to `VITE_ADMIN_ID`.
- No role/permission backend changes are required; this is a client-side gate.
- Email comparison is case-insensitive.