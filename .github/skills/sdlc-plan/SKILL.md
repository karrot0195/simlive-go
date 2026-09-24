---
name: sdlc-plan
description: 'GATE skill for planning an SDLC task. Breaks task into sub-tasks, creates wiki/workstreams/<id>/ files + INDEX.md, sets .harness/current_step.md to sdlc-implement:<task-id>:task-01. Triggers: sdlc-plan, plan task.'
argument-hint: 'sdlc-plan <task-id>'
user-invocable: true
disable-model-invocation: false
---

# sdlc-plan

- Expected prior: `sdlc-plan:<task-id>`
- Input: `wiki/tasks/<task-id>.md`
- Actions: Break into sub-tasks, create `wiki/workstreams/<task-id>/` files + INDEX.md, set `.harness/current_step.md` to `sdlc-implement:<task-id>:task-01`
