# Agentic Core: Project Context & Operations (gh-orbit)

This file defines the project-specific overrides and foundational rules for the `gh-orbit` project. These rules supplement and, where they conflict, override the generalized standards provided by the `agentic-core` extension.

## 0. Project Overview & Architecture

### 0.1 Core Philosophy

- **Local-First**: Prioritize local SQLite (`modernc.org/sqlite`, CGO-free) over API polling.
- **TUI-Centric**: Use `bubbletea`, `bubbles`, and `lipgloss` for all user interactions.
- **Observability**: Every action must be traced via `go.opentelemetry.io/otel`.
- **Zero-Config**: Credentials should be inherited from the `gh` host environment.

### 0.2 The Tech Stack

- **Language**: Go (latest stable).
- **Database**: SQLite (via `modernc.org/sqlite`).
- **TUI**: `charmbracelet/bubbletea`, `bubbles`, `lipgloss`.
- **Testing**: `testify` (use `require` for prerequisites, `assert` for results).
- **Mocks**: `mockery` (run `make generate` after interface changes).

---

## 1. Standard Operating Procedures (SOPs)

### 1.1 The Workbench Workflow

This project uses the extension's standard "Workbench" files:

- **Active Context**: `.agents/issue.md`
- **Active Proposal**: `.agents/proposal.md`
- **Active Feedback**: `.agents/feedback.md`
- **Optional RFC**: `.agents/rfc.md`

### 1.2 Task Cycle

1. **Sync**: `git pull origin main` before starting any task.
2. **Triage**: Run `make roadmap` to establish the current project state and pick the next target.
3. **Initialize**: Use `make task ID="<ID>"` to populate the context in `.agents/issue.md`.
4. **Strategy Review**: Follow the extension's **Hybrid Loop** (RFC vs Proposal) before implementation.
   - *Note*: The **Worker Role** (extension) is responsible for initializing the proposal from its own templates.
5. **Implement**: Create a topic branch via `gh issue develop <ID>`.
6. **Validate**: Run `make check` (linting + tests) after every major change.
7. **Submit**: Create a PR using `gh pr create`. Reference the Issue ID.
8. **Cleanup**: Run `make reset-task` after the PR is merged.

### 1.3 Branch Persistence

- **Stay on Branch**: NEVER switch away from a topic branch (e.g., to `main`) until you have received an explicit **SIGN-OFF** in `.agents/feedback.md` or a direct user instruction.

---

## 2. Global Rule & Skill Delegation

To minimize redundancy, this project delegates foundational logic to the `agentic-core` extension:

- **Shell Safety**: Strictly follow the high-precision piping patterns in the extension's `rules/shell-safety.md`.
- **GitHub API**: Use the extension's `github-operations` skill for all GraphQL/Rest interactions.
- **Planning & Review**: Follow the extension's `worker-role` and `reviewer-role` SIGN-OFF and Adopt/Defer protocols.

---

## 3. Sandbox & Environment Constraints (macOS Seatbelt)

AI agents operate in a restricted sandbox. You MUST adhere to these path redirections:

- **Mandatory ./tmp usage**: All caching and build artifacts MUST reside in the project-local `./tmp`.
- **Environment Variables**: Always prepend the following to shell commands:
  - `GOCACHE=$(pwd)/tmp/go-cache`
  - `GOLANGCI_LINT_CACHE=$(pwd)/tmp/lint-cache`
  - `TMPDIR=$(pwd)/tmp`
- **Self-Correction**: If you encounter "Operation not permitted," it is a signal to check your environment variable redirections and project-local paths.
