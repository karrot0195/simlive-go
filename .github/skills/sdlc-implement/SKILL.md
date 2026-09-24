---
name: sdlc-implement
description: 'GATE skill for implementing an SDLC sub-task. Reads workstream file, modifies code, runs tests, marks COMPLETED, advances .harness/current_step.md. Triggers: sdlc-implement, implement, code change.'
argument-hint: 'sdlc-implement <task-id>:<task-XX>'
user-invocable: true
disable-model-invocation: false
---

# sdlc-implement

- Expected prior: `sdlc-implement:<task-id>:task-XX`
- Input: `wiki/workstreams/<task-id>/task-XX-...md`
- Actions: Read instructions + `wiki/arch/*`, modify code, run tests, mark COMPLETED, advance `.harness/current_step.md` to next task or `sdlc-review:<task-id>`
