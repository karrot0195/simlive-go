---
name: sdlc-review
description: 'GATE skill for reviewing an SDLC task. Audits changes, verifies requirements, appends to logs.md, resets .harness/current_step.md to IDLE. Triggers: sdlc-review, review, done.'
argument-hint: 'sdlc-review <task-id>'
user-invocable: true
disable-model-invocation: false
---

# sdlc-review

- Expected prior: `sdlc-review:<task-id>`
- Input: `wiki/tasks/<task-id>.md`, `wiki/workstreams/<task-id>/*`, git diff
- Actions: Audit changes, verify requirements, generate summary, append to `logs.md`, reset `.harness/current_step.md` to `IDLE`
