# Agent Guidelines: gh-orbit

This document defines the rules and workflows for AI agents (like Gemini CLI) contributing to this project.

## 1. Core Principles

- **Local-First**: Prioritize local SQLite state over constant API polling.
- **TUI Focus**: Use `bubbletea` and `lipgloss` for all interactive elements.
- **CGO-Free**: All Go dependencies must be CGO-free (especially SQLite) to ensure easy cross-compilation.
- **Zero-Config**: Use `go-gh` for authentication. Never ask the user for a PAT.

## 2. Development Workflow

- **Roadmap-Driven**: Always refer to `.agent/implementation_plan.md` before starting a task.
- **Expertise**: Utilize the `.agent/skills/gh-extension-expert` skill for any `gh` extension specific logic.

### 2.1 Task & Branching

1. **Sync**: `git pull origin main`.
2. **Branch**: Topics like `feat/x` or `fix/y`.
3. **Strategy**: Follow [Strategy Review](.agent/workflows/strategy-review/WORKFLOW.md). **SIGN-OFF** in `.agent/proposals/` required before modifying any code files.
4. **Commits**: Conventional (feat, fix, etc.).
5. **Attribution**: End commits with `Co-authored-by: Gemini CLI <gemini-cli+noreply@google.com>`.
6 **Validation**: Proactively run `go test ./...` and `make lint` (when available) after changes.
7. **PR**: `gh pr create`. Address feedback before human review.

## 3. Tech Stack

- **Language**: Go 1.26+
- **TUI**: Charmbracelet Bubble Tea
- **Database**: `modernc.org/sqlite`
- **CLI Framework**: `spf13/cobra` (standard for `gh` extensions)
- **API**: `github.com/cli/go-gh/v2`

## 4. Interaction

- **Proactiveness**: If a roadmap task is complete, suggest the next one.
- **Mandatry agreements**: Do not proceed any tasks without the user agreements.
- **Clarity**: Always explain the intent of a shell command before execution.
