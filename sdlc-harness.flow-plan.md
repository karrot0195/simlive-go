# SDLC Harness Flow
> Status: IMPLEMENTED (TASK-101-sdlc-harness-bootstrap bootstrap complete). See `.harness/current_step.md` (`IDLE`), `instructions/*.md`, `wiki/tasks/TASK-101-sdlc-harness-bootstrap.md`, `wiki/workstreams/TASK-101-sdlc-harness-bootstrap/`, `logs.md`.

## Global Execution Guard
Every skill MUST perform the following validations prior to execution:
1. **Context Loading:** Load all architectural boundaries from `wiki/arch/*` and execution guidelines from `instructions/*.md`.
2. **State Validation:** Read `.harness/current_step.md`. If the requested skill does not match the active step allowed by `flow.md`, HALT immediately and log an out-of-sequence error.

---

## SDLC Pipeline State Machine

[sdlc-task-request] ──> [sdlc-plan] ──> [sdlc-implement] ──> [sdlc-review]


### 1. `sdlc-task-request`
- **Expected Prior Step:** None (Initiation) or `sdlc-review` (New Task)
- **Input:** High-level task summary or issue description.
- **Actions:**
  1. Scan existing codebase to understand context and dependencies.
  2. Generate a high-level summary plan.
  3. Generate a unique task ID (e.g., `TASK-101-sdlc-harness-bootstrap`).
  4. Create task specification file at `wiki/tasks/<task-id>.md`.
  5. Update `.harness/current_step.md` to `sdlc-plan:<task-id>`.
- **Output Artifact:** `wiki/tasks/<task-id>.md`

---

### 2. `sdlc-plan`
- **Expected Prior Step:** `sdlc-task-request:<task-id>`
- **Input:** `wiki/tasks/<task-id>.md`
- **Actions:**
  1. Parse the summary plan from `wiki/tasks/<task-id>.md`.
  2. Break down the task into atomic, sequentially executable sub-tasks.
  3. Create individual step-by-step workstream files under `wiki/workstreams/<task-id>/`.
  4. Create `wiki/workstreams/<task-id>/INDEX.md` listing the execution order (e.g., `task-01-setup.md`, `task-02-api-impl.md`).
  5. Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-01`.
- **Output Artifacts:** `wiki/workstreams/<task-id>/task-01-{short-description}.md`, `task-02-...`

---

### 3. `sdlc-implement`
- **Expected Prior Step:** `sdlc-plan:<task-id>` or `sdlc-implement:<task-id>:task-N`
- **Input:** Target workstream file (`wiki/workstreams/<task-id>/task-XX-{short-description}.md`).
- **Actions:**
  1. Read target sub-task instructions and architectural boundaries from `wiki/arch/*`.
  2. Execute code modifications and unit tests for the current step.
  3. Mark the current sub-task file status as `COMPLETED`.
  4. If more sub-tasks exist:
     - Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-N+1`.
  5. If all sub-tasks in `wiki/workstreams/<task-id>/` are complete:
     - Update `.harness/current_step.md` to `sdlc-review:<task-id>`.
- **Output Artifacts:** Code updates, updated sub-task statuses.

---

### 4. `sdlc-review`
- **Expected Prior Step:** `sdlc-implement:<task-id>:completed`
- **Input:** `wiki/tasks/<task-id>.md`, `wiki/workstreams/<task-id>/*`, Git diff.
- **Actions:**
  1. Audit code changes against the initial summary plan in `wiki/tasks/<task-id>.md` and architecture rules in `wiki/arch/*`.
  2. Verify all requirements and edge cases are satisfied.
  3. Generate an execution summary of what was implemented.
  4. Append the summary to `logs.md` using the strict schema below.
  5. Reset `.harness/current_step.md` to `IDLE`.
- **Output Artifact:** Entry appended to `logs.md`

---

## Log Schema (`logs.md`)

All completions MUST append to `logs.md` in the following exact format:

```text
--- YYYY-MM-DD HH:mm:ss ---
summary: [TASK-ID] Brief 1-line description of the outcome
detail:
  - Task ID: <task-id>
  - Planned vs Implemented: Match status and deviations
  - Modified Files: List of changed files
  - Key Changes: High-level technical choices made

---

### Key Improvements Made

1. **State String Parameterization:** Instead of generic step names, `.harness/current_step.md` holds parameterized states (e.g., `sdlc-implement:TASK-101-sdlc-harness-bootstrap:task-02`), ensuring sub-tasks are tracked accurately without skipping.
2. **Index File for Workstreams:** Added `wiki/workstreams/<task-id>/INDEX.md` so the implementing AI agent always knows the exact sequence and remaining sub-tasks.
3. **Formalized GATE Checks:** Standardized the guard logic at the beginning of `.harness/flow.md` so all agent operations enforce reading `wiki/arch/*` and `instructions/*.md`.
4. **Structured Log Format:** Refined the `logs.md` format into key-value pairs to prevent free-form text drift across iterations.

<FollowUp label="Would you like me to write prompt instructions for each of the 4 GATE skills?" query="Write prompt instructions for the 4 SDLC GATE skills (sdlc-task-request, sdlc-plan, sdlc-implement, sdlc-review) based on this harness flow."/>
generate to final markdown -> can download

Which response is more helpful?
Your choice will help Gemini give you more helpful responses. Loading the response may take a moment.
Choice A
Python
# Create the markdown file content based on the improved SDLC Harness Flow
md_content = """# SDLC Harness Flow & Architecture Specification

> **Status:** Active Standard  
> **Location:** `.harness/flow.md`  
> **Purpose:** Single source of truth for AI-driven software development lifecycle (SDLC) automation and state tracking.

---

## 1. Global Execution Guard (GATE)

Every skill MUST perform the following checks prior to executing any action:

1. **Context Loading:**
   - Load all system architecture guidelines from `wiki/arch/*`.
   - Load project instruction files from `instructions/*.md`.
2. **State & Sequence Validation:**
   - Read `.harness/current_step.md`.
   - Validate that the requested skill aligns exactly with the allowed state transitions defined in `.harness/flow.md`.
   - **Strict Rule:** If the current request does NOT match `.harness/current_step.md`, **HALT execution immediately**, log an out-of-sequence error, and refuse to proceed.

---

## 2. SDLC Pipeline State Machine

[sdlc-task-request] ──► [sdlc-plan] ──► [sdlc-implement] ──► [sdlc-review]


---

### Step 1: `sdlc-task-request`

* **Target Gate:** `sdlc-task-request`
* **Expected Prior Step:** `IDLE` or `sdlc-review:<previous-task-id>:complete`
* **Input:** High-level feature request, bug summary, or task description.

#### Actions:
1. Scan current codebase context, dependencies, and architecture patterns (`wiki/arch/*`).
2. Draft a high-level technical summary plan addressing the request.
3. Assign a unique task identifier (e.g., `TASK-101-sdlc-harness-bootstrap`).
4. Create the main task specification file at `wiki/tasks/<task-id>.md`.
5. Write `sdlc-plan:<task-id>` into `.harness/current_step.md`.

#### Output Artifact:
* `wiki/tasks/<task-id>.md`

---

### Step 2: `sdlc-plan`

* **Target Gate:** `sdlc-plan`
* **Expected Prior Step:** `sdlc-plan:<task-id>`
* **Input:** `wiki/tasks/<task-id>.md`

#### Actions:
1. Parse summary plan and requirements from `wiki/tasks/<task-id>.md`.
2. Split the core implementation into small, atomic, and incrementally executable sub-tasks.
3. Create individual step-by-step workstream files under `wiki/workstreams/<task-id>/`.
4. Create an `INDEX.md` manifest listing execution order:
   * `wiki/workstreams/<task-id>/INDEX.md`
   * `wiki/workstreams/<task-id>/task-01-{short-description}.md`
   * `wiki/workstreams/<task-id>/task-02-{short-description}.md`
5. Write `sdlc-implement:<task-id>:task-01` into `.harness/current_step.md`.

#### Output Artifacts:
* `wiki/workstreams/<task-id>/INDEX.md`
* `wiki/workstreams/<task-id>/task-01-{short-description}.md` ... `task-N`

---

### Step 3: `sdlc-implement`

* **Target Gate:** `sdlc-implement`
* **Expected Prior Step:** `sdlc-implement:<task-id>:task-XX`
* **Input:** Active workstream file (`wiki/workstreams/<task-id>/task-XX-{short-description}.md`)

#### Actions:
1. Read step instructions and enforce boundary constraints from `wiki/arch/*` and `instructions/*.md`.
2. Write/modify code and execute associated unit or integration tests for the active step.
3. Update active sub-task state to `COMPLETED` in its workstream document.
4. **State Transition Logic:**
   * **If sub-tasks remain:** Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-N+1`.
   * **If all sub-tasks complete:** Update `.harness/current_step.md` to `sdlc-review:<task-id>`.

#### Output Artifacts:
* Source code changes and unit tests.
* Updated workstream status tags.

---

### Step 4: `sdlc-review`

* **Target Gate:** `sdlc-review`
* **Expected Prior Step:** `sdlc-review:<task-id>`
* **Input:** `wiki/tasks/<task-id>.md`, `wiki/workstreams/<task-id>/*`, and `git diff`.

#### Actions:
1. Perform an audit comparing code diffs against the summary plan (`wiki/tasks/<task-id>.md`) and architecture guidelines (`wiki/arch/*`).
2. Verify all functional and edge-case requirements are satisfied.
3. Generate a release/implementation summary.
4. Append execution results to `logs.md` using the strict schema.
5. Reset `.harness/current_step.md` to `IDLE`.

#### Output Artifact:
* Audit entry in `logs.md`.

---

## 3. Mandatory Log Schema (`logs.md`)

All completions MUST append to `logs.md` using this strict schema:

```text
--- YYYY-MM-DD HH:mm:ss ---
summary: [<task-id>] Concise summary of implemented feature or bug fix
detail:
  - Task ID: <task-id>
  - Planned vs Implemented: Match status and details on any intentional deviations
  - Modified Files:
    - path/to/file1.ext
    - path/to/file2.ext
  - Key Changes: High-level architectural or code choices made during execution
4. Skill Instructions & Prompts Specification
Below are explicit system instruction templates for each GATE skill to ensure deterministic execution.

Skill 1: sdlc-task-request
Markdown
You are executing the `sdlc-task-request` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state is `IDLE` or previous review complete. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Scan code context relevant to the incoming prompt.
5. Create `wiki/tasks/<task-id>.md` containing task summary and scope.
6. Write `sdlc-plan:<task-id>` to `.harness/current_step.md`.
Skill 2: sdlc-plan
Markdown
You are executing the `sdlc-plan` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-plan:<task-id>`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Parse `wiki/tasks/<task-id>.md`.
5. Create `wiki/workstreams/<task-id>/INDEX.md` and `task-XX-{short-description}.md` files.
6. Write `sdlc-implement:<task-id>:task-01` to `.harness/current_step.md`.
Skill 3: sdlc-implement
Markdown
You are executing the `sdlc-implement` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-implement:<task-id>:task-XX`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Execute code changes described in `wiki/workstreams/<task-id>/task-XX-{short-description}.md`.
5. Validate implementation with tests.
6. Update `.harness/current_step.md` to next sub-task or `sdlc-review:<task-id>`.
Skill 4: sdlc-review
Markdown
You are executing the `sdlc-review` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-review:<task-id>`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Review diffs against `wiki/tasks/<task-id>.md`.
5. Append structured entry to `logs.md`.
6. Write `IDLE` to `.harness/current_step.md`.
"""

file_path = "sdlc_harness_flow.md"
with open(file_path, "w", encoding="utf-8") as f:
f.write(md_content)

print(f"FILE_GENERATED: {file_path}")


```text?code_stdout&code_event_index=2
FILE_GENERATED: sdlc_harness_flow.md

Your Markdown file is ready

MD icon
sdlc_harness_flow
 MD 
File Overview (sdlc_harness_flow.md)
The generated markdown document includes:

Global Execution Guard (GATE): Enforces mandatory context loading (wiki/arch/* + instructions/*.md) and strict state checks against .harness/current_step.md.

SDLC Pipeline State Machine: Clear step-by-step inputs, actions, state transitions, and output artifacts for:

sdlc-task-request

sdlc-plan

sdlc-implement

sdlc-review

Log Schema (logs.md): Fixed structured format for progress tracking.

Skill Prompt Specifications: Instructions for each of the 4 GATE skills to ensure predictable execution by AI agents.

Choice B
Python
# Write the markdown content to a downloadable .md file
content = """# SDLC Harness Flow

## Global Execution Guard
Every skill MUST perform the following validations prior to execution:
1. **Context Loading:** Load all architectural boundaries from `wiki/arch/*` and execution guidelines from `instructions/*.md`.
2. **State Validation:** Read `.harness/current_step.md`. If the requested skill does not match the active step allowed by `flow.md`, HALT immediately and log an out-of-sequence error.

---

## SDLC Pipeline State Machine

[sdlc-task-request] ──> [sdlc-plan] ──> [sdlc-implement] ──> [sdlc-review]


### 1. `sdlc-task-request`
- **Expected Prior Step:** None (Initiation) or `sdlc-review` (New Task)
- **Input:** High-level task summary or issue description.
- **Actions:**
  1. Scan existing codebase to understand context and dependencies.
  2. Generate a high-level summary plan.
  3. Generate a unique task ID (e.g., `TASK-101-sdlc-harness-bootstrap`).
  4. Create task specification file at `wiki/tasks/<task-id>.md`.
  5. Update `.harness/current_step.md` to `sdlc-plan:<task-id>`.
- **Output Artifact:** `wiki/tasks/<task-id>.md`

---

### 2. `sdlc-plan`
- **Expected Prior Step:** `sdlc-task-request:<task-id>`
- **Input:** `wiki/tasks/<task-id>.md`
- **Actions:**
  1. Parse the summary plan from `wiki/tasks/<task-id>.md`.
  2. Break down the task into atomic, sequentially executable sub-tasks.
  3. Create individual step-by-step workstream files under `wiki/workstreams/<task-id>/`.
  4. Create `wiki/workstreams/<task-id>/INDEX.md` listing the execution order (e.g., `task-01-setup.md`, `task-02-api-impl.md`).
  5. Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-01`.
- **Output Artifacts:** `wiki/workstreams/<task-id>/task-01-{short-description}.md`, `task-02-...`

---

### 3. `sdlc-implement`
- **Expected Prior Step:** `sdlc-plan:<task-id>` or `sdlc-implement:<task-id>:task-N`
- **Input:** Target workstream file (`wiki/workstreams/<task-id>/task-XX-{short-description}.md`).
- **Actions:**
  1. Read target sub-task instructions and architectural boundaries from `wiki/arch/*`.
  2. Execute code modifications and unit tests for the current step.
  3. Mark the current sub-task file status as `COMPLETED`.
  4. If more sub-tasks exist:
     - Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-N+1`.
  5. If all sub-tasks in `wiki/workstreams/<task-id>/` are complete:
     - Update `.harness/current_step.md` to `sdlc-review:<task-id>`.
- **Output Artifacts:** Code updates, updated sub-task statuses.

---

### 4. `sdlc-review`
- **Expected Prior Step:** `sdlc-implement:<task-id>:completed`
- **Input:** `wiki/tasks/<task-id>.md`, `wiki/workstreams/<task-id>/*`, Git diff.
- **Actions:**
  1. Audit code changes against the initial summary plan in `wiki/tasks/<task-id>.md` and architecture rules in `wiki/arch/*`.
  2. Verify all requirements and edge cases are satisfied.
  3. Generate an execution summary of what was implemented.
  4. Append the summary to `logs.md` using the strict schema below.
  5. Reset `.harness/current_step.md` to `IDLE`.
- **Output Artifact:** Entry appended to `logs.md`

---

## Log Schema (`logs.md`)

All completions MUST append to `logs.md` in the following exact format:

```text
--- YYYY-MM-DD HH:mm:ss ---
summary: [TASK-ID] Brief 1-line description of the outcome
detail:
  - Task ID: <task-id>
  - Planned vs Implemented: Match status and deviations
  - Modified Files: List of changed files
  - Key Changes: High-level technical choices made
"""

file_path = "flow.md"
with open(file_path, "w", encoding="utf-8") as f:
f.write(content)

print(f"File created successfully: {file_path}")


```text?code_stdout&code_event_index=1
File created successfully: flow.md

Your Markdown file is ready

MD icon
flow
 MD 
You can download flow.md above to place into your .harness/ directory.




Gemini is AI and can make mistakes.

Analyzing
# SDLC Harness Flow & Architecture Specification

> **Status:** Active Standard  
> **Location:** `.harness/flow.md`  
> **Purpose:** Single source of truth for AI-driven software development lifecycle (SDLC) automation and state tracking.

---

## 1. Global Execution Guard (GATE)

Every skill MUST perform the following checks prior to executing any action:

1. **Context Loading:**
   - Load all system architecture guidelines from `wiki/arch/*`.
   - Load project instruction files from `instructions/*.md`.
2. **State & Sequence Validation:**
   - Read `.harness/current_step.md`.
   - Validate that the requested skill aligns exactly with the allowed state transitions defined in `.harness/flow.md`.
   - **Strict Rule:** If the current request does NOT match `.harness/current_step.md`, **HALT execution immediately**, log an out-of-sequence error, and refuse to proceed.

---

## 2. SDLC Pipeline State Machine

```
[sdlc-task-request] ──► [sdlc-plan] ──► [sdlc-implement] ──► [sdlc-review]
```

---

### Step 1: `sdlc-task-request`

* **Target Gate:** `sdlc-task-request`
* **Expected Prior Step:** `IDLE` or `sdlc-review:<previous-task-id>:complete`
* **Input:** High-level feature request, bug summary, or task description.

#### Actions:
1. Scan current codebase context, dependencies, and architecture patterns (`wiki/arch/*`).
2. Draft a high-level technical summary plan addressing the request.
3. Assign a unique task identifier (e.g., `TASK-101-sdlc-harness-bootstrap`).
4. Create the main task specification file at `wiki/tasks/<task-id>.md`.
5. Write `sdlc-plan:<task-id>` into `.harness/current_step.md`.

#### Output Artifact:
* `wiki/tasks/<task-id>.md`

---

### Step 2: `sdlc-plan`

* **Target Gate:** `sdlc-plan`
* **Expected Prior Step:** `sdlc-plan:<task-id>`
* **Input:** `wiki/tasks/<task-id>.md`

#### Actions:
1. Parse summary plan and requirements from `wiki/tasks/<task-id>.md`.
2. Split the core implementation into small, atomic, and incrementally executable sub-tasks.
3. Create individual step-by-step workstream files under `wiki/workstreams/<task-id>/`.
4. Create an `INDEX.md` manifest listing execution order:
   * `wiki/workstreams/<task-id>/INDEX.md`
   * `wiki/workstreams/<task-id>/task-01-{short-description}.md`
   * `wiki/workstreams/<task-id>/task-02-{short-description}.md`
5. Write `sdlc-implement:<task-id>:task-01` into `.harness/current_step.md`.

#### Output Artifacts:
* `wiki/workstreams/<task-id>/INDEX.md`
* `wiki/workstreams/<task-id>/task-01-{short-description}.md` ... `task-N`

---

### Step 3: `sdlc-implement`

* **Target Gate:** `sdlc-implement`
* **Expected Prior Step:** `sdlc-implement:<task-id>:task-XX`
* **Input:** Active workstream file (`wiki/workstreams/<task-id>/task-XX-{short-description}.md`)

#### Actions:
1. Read step instructions and enforce boundary constraints from `wiki/arch/*` and `instructions/*.md`.
2. Write/modify code and execute associated unit or integration tests for the active step.
3. Update active sub-task state to `COMPLETED` in its workstream document.
4. **State Transition Logic:**
   * **If sub-tasks remain:** Update `.harness/current_step.md` to `sdlc-implement:<task-id>:task-N+1`.
   * **If all sub-tasks complete:** Update `.harness/current_step.md` to `sdlc-review:<task-id>`.

#### Output Artifacts:
* Source code changes and unit tests.
* Updated workstream status tags.

---

### Step 4: `sdlc-review`

* **Target Gate:** `sdlc-review`
* **Expected Prior Step:** `sdlc-review:<task-id>`
* **Input:** `wiki/tasks/<task-id>.md`, `wiki/workstreams/<task-id>/*`, and `git diff`.

#### Actions:
1. Perform an audit comparing code diffs against the summary plan (`wiki/tasks/<task-id>.md`) and architecture guidelines (`wiki/arch/*`).
2. Verify all functional and edge-case requirements are satisfied.
3. Generate a release/implementation summary.
4. Append execution results to `logs.md` using the strict schema.
5. Reset `.harness/current_step.md` to `IDLE`.

#### Output Artifact:
* Audit entry in `logs.md`.

---

## 3. Mandatory Log Schema (`logs.md`)

All completions MUST append to `logs.md` using this strict schema:

```text
--- YYYY-MM-DD HH:mm:ss ---
summary: [<task-id>] Concise summary of implemented feature or bug fix
detail:
  - Task ID: <task-id>
  - Planned vs Implemented: Match status and details on any intentional deviations
  - Modified Files:
    - path/to/file1.ext
    - path/to/file2.ext
  - Key Changes: High-level architectural or code choices made during execution
```

---

## 4. Skill Instructions & Prompts Specification

Below are explicit system instruction templates for each GATE skill to ensure deterministic execution.

### Skill 1: `sdlc-task-request`
```markdown
You are executing the `sdlc-task-request` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state is `IDLE` or previous review complete. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Scan code context relevant to the incoming prompt.
5. Create `wiki/tasks/<task-id>.md` containing task summary and scope.
6. Write `sdlc-plan:<task-id>` to `.harness/current_step.md`.
```

### Skill 2: `sdlc-plan`
```markdown
You are executing the `sdlc-plan` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-plan:<task-id>`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Parse `wiki/tasks/<task-id>.md`.
5. Create `wiki/workstreams/<task-id>/INDEX.md` and `task-XX-{short-description}.md` files.
6. Write `sdlc-implement:<task-id>:task-01` to `.harness/current_step.md`.
```

### Skill 3: `sdlc-implement`
```markdown
You are executing the `sdlc-implement` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-implement:<task-id>:task-XX`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Execute code changes described in `wiki/workstreams/<task-id>/task-XX-{short-description}.md`.
5. Validate implementation with tests.
6. Update `.harness/current_step.md` to next sub-task or `sdlc-review:<task-id>`.
```

### Skill 4: `sdlc-review`
```markdown
You are executing the `sdlc-review` gate.
1. Read `.harness/current_step.md` and `.harness/flow.md`.
2. Verify state matches `sdlc-review:<task-id>`. If not, HALT.
3. Load `wiki/arch/*` and `instructions/*.md`.
4. Review diffs against `wiki/tasks/<task-id>.md`.
5. Append structured entry to `logs.md`.
6. Write `IDLE` to `.harness/current_step.md`.
```
sdlc_harness_flow.md
Displaying sdlc_harness_flow.md.