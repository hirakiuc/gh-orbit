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

### 2.5 Sandbox & macOS Seatbelt

This project is configured with a restricted sandbox for AI tools (macOS Seatbelt). 

- **Operation not permitted**: If you encounter this error (or "Permission denied") when running shell commands, it is likely due to sandbox constraints. Do not attempt to work around these by modifying system paths.
- **Mandatory ./tmp usage**: You MUST use the project's `./tmp` directory for all caches and transient files.
- **Environment Variables**: Always prepend or export the following when running build or test tools:
  - `GOCACHE=$(pwd)/tmp/go-cache`
  - `GOLANGCI_LINT_CACHE=$(pwd)/tmp/lint-cache`
  - `TMPDIR=$(pwd)/tmp`
- **Prefer Makefile**: Use `make` targets (e.g., `make build`, `make test`, `make lint`) as they are already configured to respect these sandbox-friendly paths.
