# Gemini CLI Mandates

These instructions are foundational mandates and take absolute precedence.

## 1. General Principles

Refer to [AGENTS.md](AGENTS.md) for core project principles, tech stack, and development workflows.

## 2. Gemini Specific Guidelines

### 2.1 Commit Attribution

Always include the following co-authored-by footer in commit messages:
`Co-authored-by: Gemini CLI <gemini-cli+noreply@google.com>`

### 2.2 Shell Safety

Strictly follow the shell safety protocols defined in [.agent/rules/shell-safety.md](.agent/rules/shell-safety.md).

### 2.3 Strategy Review

Mandatory adherence to the [Strategy Review Workflow](.agent/workflows/strategy-review/WORKFLOW.md) before any changes to `internal/` or `cmd/`.

### 2.4 Reviewer Plan Mode

When acting as a Reviewer, you should use `enter_plan_mode` for the research and analysis phase. Use this phase to read source files and perform online research (`google_web_search`, `web_fetch`) for latest best practices. Since Plan Mode prevents all file writes, you must finalize your analysis and then transition to active mode strictly to write your findings to `.agents/feedback.md`.
