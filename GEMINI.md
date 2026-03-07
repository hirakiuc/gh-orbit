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
