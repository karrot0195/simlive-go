# Phase 1 Data Model: Planner & Engine Channel Lifecycle Reactivity

**Feature**: `013-planner-channel-lifecycle` | **Date**: 2026-11-03

## 1. Channel entity (`playout_channels`)

Existing table; one new field modeled explicitly.

| Field | Type | Source | Rules |
|---|---|---|---|
| `id` | varchar(36) PK | existing | Immutable channel identifier |
| `name` | varchar(255) | existing | Definition field; change restarts work |
| `base_name` | varchar(255) | existing | Definition field; change restarts work |
| `content_timezone` | varchar(50) | existing | Definition field; change restarts work |
| `channel_epg_id` | varchar(36) | existing | Definition field; change restarts work |
| `use_ad_epg_ads` | boolean | existing | Definition field; change restarts work |
| `status` | varchar(20) | existing enum | One of `PREPARE`, `LIVE`, `INACTIVE`, `FAILURE` |
| `label` | varchar(255) | existing | Human-readable failure/last-action label |
| `is_active` | boolean NOT NULL DEFAULT true | **now modeled in Go** | Operator enable switch; previously only addressed by raw column name |
| `created_at` / `updated_at` | timestamps | existing | `updated_at` bumped on every lifecycle mutation |

**New Go field**: `PlayoutChannel.IsActive bool` with `gorm:"column:is_active;not null;default:true"` and `json:"is_active"`.

**Migration note**: All three binaries run `AutoMigrate(&models.PlayoutChannel{})`. Adding the field creates `is_active` if the deployed database lacks it. Existing rows receive the column default (`true`); rows already stopped are excluded by their `status`, and rows previously failure-disabled retain their stored `false`.

## 2. Active predicate

```
isActive(channel) := channel.is_active == true
                     AND channel.status NOT IN ('INACTIVE', 'FAILURE')
```

Applied identically by:
- Planner roster reconciliation
- Engine roster reconciliation
- Planner lifecycle writes that may reactivate a channel (conditional update)
- API purge eligibility and any active-channel listings that already use `is_active`

The predicate is the single source of truth for FR-001, FR-004, and FR-007.

## 3. Lifecycle state machine

### Operator/API transitions

| From | Action | To (`status`, `is_active`) | Durable rule |
|---|---|---|---|
| absent | Create/upsert channel | `PREPARE`, `true` | Idempotent upsert |
| any | Start | `PREPARE`, `true` | Idempotent; successful state wins |
| any | Stop | `INACTIVE`, `false` | Cannot be undone by in-flight work |
| `INACTIVE`/`FAILURE`/`LIVE`/`PREPARE` | PUT/PATCH status `PREPARE` or `LIVE` | requested status, `true` | Reactivation path |
| `LIVE`/`PREPARE` | PUT/PATCH status `INACTIVE` | `INACTIVE`, `false` | Equivalent to Stop |
| `INACTIVE`/`FAILURE` | PUT/PATCH `is_active=true` | `PREPARE`, `true` | Equivalent to Start |
| any | PUT/PATCH `is_active=false` | `INACTIVE`, `false` | Equivalent to Stop |
| any | Delete | row removed | Hard delete; cancels work and purges output |

### System transitions

| From | Trigger | To | Guard |
|---|---|---|---|
| `PREPARE` | Prepare succeeds and timeline present | `LIVE` | Conditional update: row must still be active |
| `PREPARE`/`LIVE` | Last prepare fails | stays (`PREPARE`/`LIVE`) | Failure counter increments; after threshold → `is_active=false` |
| `PREPARE`/`LIVE` | Failure threshold reached | `is_active=false` (status unchanged) | Predicate marks inactive; no resurrection |
| any active | Worker cancelled | unchanged | Must not write `LIVE` after cancellation |

**Invariant**: no operation may write `status='LIVE'` or set `is_active=true` unless it is an explicit operator action.

## 4. Work-session (supervisor) model

In-memory only, per service; not persisted.

| Field | Meaning |
|---|---|
| `channel` | Last observed channel definition |
| `cancel` | Cancel function for the work context |
| `done` | Closed when the work unit has fully exited |
| `state` | `running`, `stopping`, `restart_pending` |
| `generation` | Increments per start; guards stale cleanup/writes |

**Rules**:
- At most one running work unit per channel.
- `stopping` entries are not replaced until `done` is closed.
- Reactivation while `stopping` sets `restart_pending`; a new unit starts after the old one exits.
- Cleanup always runs after the work unit exits, never concurrently with it.
- Definition change is a stop+start with cleanup.

## 5. Redis state lifecycle

| Key | Writer | Stop/delete handling |
|---|---|---|
| `simlive:timeline:{id}` (ZSET) | planner | delete on stop/removal |
| `planner:cursor:{id}` | planner | delete on stop/removal |
| `planner:failures:{id}` | planner | delete on stop/removal during planner exit; preserved by API stop/delete per documented contract |
| `planner:fallback:{id}` | planner | delete on stop/removal with timeline data |
| `simlive:hls:{id}` (master) | engine | delete on stop/removal |
| `simlive:hls:{id}:rendition:{rung}` | engine | delete on stop/removal |
| `simlive:renditions:{id}` (metadata) | engine | delete on stop/removal |
| `TMP_DIR/{id}/**` | engine/planner | remove on stop/removal |

**Ordering**: API purge (best effort, immediate) → engine purge (after playout loop exits) → planner cleanup (after prepare unit exits). All purges are idempotent; a subsequent start rebuilds the state.

## 6. Validation rules

- `status` must be one of `PREPARE`, `LIVE`, `INACTIVE`; `FAILURE` is system-written only.
- If a request supplies both `status` and `is_active` and they contradict, reject with `400`.
- `is_active=true` with status `INACTIVE` is normalized to `PREPARE`; `is_active=false` with status `PREPARE`/`LIVE` is normalized to `INACTIVE` on the same request.
- Channel identifiers are validated by the existing path guard and ESPX lookup for creation.
- The roster query returns only channels satisfying the active predicate, ordered deterministically (by `id`) so diffs are stable.
