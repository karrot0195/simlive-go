---
name: doc-generator
description: Generate or update project documentation (such as README.md, FLOW.md, or provider specs) based on the repository tree, codebase features, tech stack, and workflows. Use when the user asks to create, update, or maintain documentation files.
---
# Documentation Generator Skill

A skill for generating, updating, and structuring project documentation files like `README.md`, `FLOW.md`, or architecture specifications.

## When to Use
Use this skill when the user asks to create or update project documentation, workflow files, structure docs, or markdown guides based on the active codebase layout and design.

## Workflow

1. **Analyze Workspace & Scope**: Review the repository tree, source files, directory layout (e.g., `docs/`, `internal/`, `bin/`), and build configurations (`makefile`).
2. **Determine Target Document**: Identify whether you are updating `README.md`, creating a flow document (`docs/FLOW.md`), or documenting a provider spec.
3. **Synthesize Content**: 
   - Ensure accurate stack details and setup/build steps (`make build`, `./bin/...`).
   - Match exact directory trees and schema references.
4. **Write/Update Markdown**: Generate clear, scannable markdown files using proper headings, code blocks, and lists.