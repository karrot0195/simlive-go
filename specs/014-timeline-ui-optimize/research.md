# Research: Timeline UI Optimization

## Decision: Date formatting approach

- **Decision**: Reuse the existing `toLocaleTimeString()`-based formatting but switch to `toLocaleString()` with date components when the range spans more than one calendar day. Per-entry labels show MM-dd only when the entry's calendar day differs from the previous entry's day.
- **Rationale**: Matches the user's "MM-dd" expectation while keeping single-day views compact and consistent with existing locale output.
- **Alternatives considered**: Always show full date — rejected as noisy for the common single-day case.

## Decision: Lazy loading mechanism

- **Decision**: Client-side pagination with a "Load more" button. Track a per-channel `loadedCount` ref (default page size 50). Render `entries.slice(0, loadedCount)`. Reset `loadedCount` to page size on SSE/manual refresh.
- **Rationale**: Data is already fetched in full via SSE; pagination avoids DOM/render cost without backend changes. "Load more" is predictable and testable.
- **Alternatives considered**: Infinite scroll — deferred; button is simpler and avoids scroll-event complexity. API-side pagination — deferred per spec assumptions.

## Decision: Gap detection scope

- **Decision**: Compute gap flags over the full ordered entry list (not the rendered subset) so boundary gaps survive lazy loading. Store a `Set` of sequence numbers (or indices) flagged as gap starts, recomputed when entries change.
- **Rationale**: Preserves existing behavior while keeping render cheap.
- **Alternatives considered**: Recompute per rendered entry using full list lookup — rejected as O(n) per render.

## Decision: Page size

- **Decision**: Named constant `TIMELINE_PAGE_SIZE = 50`, single source of truth in the component.
- **Rationale**: Matches spec default; easily adjustable.