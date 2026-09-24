---
name: sdlc-task-request
description: 'GATE skill for initiating a new SDLC task. Scans codebase, generates plan, assigns TASK-ID, creates wiki/tasks/<id>.md, sets .harness/current_step.md to sdlc-plan:<task-id>. Triggers: sdlc-task-request, new task, task request.'
argument-hint: 'sdlc-task-request [task description]'
user-invocable: true
disable-model-invocation: false
---

# sdlc-task-request

- Expected prior: `IDLE` or `sdlc-review:<task-id>:complete`
- Input: High-level task description
- Actions: Scan codebase, generate plan, assign TASK-ID, create `wiki/tasks/<id>.md`, set `.harness/current_step.md` to `sdlc-plan:<task-id>`
- Halt if state does not match.
