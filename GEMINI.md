# Gemini CLI Mandates

These instructions are foundational mandates and take absolute precedence.

## 1. General Principles

Refer to [AGENTS.md](AGENTS.md) for core project principles, tech stack, and development workflows.

## 2. Gemini Specific Guidelines

### 2.1 Commit Attribution

Always include the following co-authored-by footer in commit messages:
`Co-authored-by: Gemini CLI <gemini-cli+noreply@google.com>`

### 2.2 Shell Safety

Strictly follow the shell safety protocols defined by the **agentic-core** extension.

### 2.3 Strategy Review

Mandatory adherence to the **Strategy Review Workflow** provided by the **agentic-core** extension before any changes to `internal/`, `cmd/`, or `native/`.

### 2.4 Sandbox & macOS Seatbelt

This project is configured with a restricted sandbox for AI tools. You MUST strictly follow the environment constraints and path redirections defined in [AGENTS.md](AGENTS.md#2-sandbox--environment-constraints-macos-seatbelt).

## 3. Security Boundary Note

Be aware that the **Orbit Cockpit (macOS)** native component has sandboxing disabled for technical reasons (PTY management). While you may modify its code, you must remain within the restricted Seatbelt environment for all tool executions.
