---
type: project-knowledge
schema_version: 1
project: simlive
---

# Retry Logic

The planner wraps EPG processing with exponential backoff retry. Configurable via planner settings.

Parent: [[planner]]

## Behavior

- `MaxPrepareRetries` controls the number of attempts (default 1 if unset).
- Base delay and max delay are configurable via `PrepareRetryBaseMs` and `PrepareRetryMaxMs`.
- Delay doubles each attempt: `base * (1 << (i - 1))`, capped at `maxDelay`.
- `UnspportStreamSchema` errors break immediately (no retry).
- Context cancellation aborts the retry loop.

## Related

- [[epg-processing]] — the operation being retried
- [[config]] — planner settings struct