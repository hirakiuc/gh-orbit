# Agentic Core: Project Context & Operations (gh-orbit)

This file defines the project-specific overrides and foundational rules for the `gh-orbit` project. These rules supplement and, where they conflict, override the generalized standards provided by the `agentic-core` extension.

## 0. Project Overview & Architecture

### 0.1 Core Philosophy

- **Local-First**: Prioritize local SQLite (`modernc.org/sqlite`, CGO-free) over API polling.
- **TUI-Centric**: Bubbletea-based interface for rapid terminal triage.
- **Hybrid Native**: Modular macOS "Cockpit" for high-density multi-agent orchestration.
- **Observability**: Every mutation must be signaled via the internal EventBus and reflected in MCP.

### 0.2 The Tech Stack

- **Backend**: Go (latest stable).
- **Protocol**: **Model Context Protocol (MCP)** over secure Unix Domain Sockets.
- **Database**: SQLite (via `modernc.org/sqlite`).
- **TUI**: `charmbracelet/bubbletea`.
- **Native macOS**: SwiftUI, Swift 6 (Strict Concurrency), SwiftTerm.
- **Testing**: `testify` (Go), **Swift Testing** (Native).
- **Mocks**: `mockery` (run `make go/generate` after interface changes).

---

## 1. Standard Operating Procedures (SOPs)

### 1.1 The Workbench Workflow

This project uses the extension's standard "Workbench" files:

- **Active Context**: `.agents/issue.md`
- **Active Proposal**: `.agents/proposal.md`
- **Active Feedback**: `.agents/feedback.md`
- **Optional RFC**: `.agents/rfc.md`

### 1.2 Task Cycle

1. **Sync**: `git checkout main && git pull origin main` before starting any task.
2. **Triage**: Run `make roadmap` to establish the current project state and pick the next target.
3. **Initialize**: Use `make task ID="<ID>"` to populate the context in `.agents/issue.md`.
4. **Strategy Review**: Follow the extension's **Hybrid Loop** (RFC vs Proposal) before implementation.
5. **Implement**: Create a topic branch via `gh issue develop <ID>`.
6. **Validate**: Run **`make check`** (all languages) after every major change.
7. **Submit**: Create a PR using `gh pr create`. Reference the Issue ID.
8. **Cleanup**: Run `make reset-task` after the PR is merged.

### 1.3 Quality Policy (Always Green)

- **Unfailing Gates**: `make check` is the single source of truth. It orchestrates `go/check` and `native/check`.
- **PR Requirement**: No Pull Request will be merged unless `make check` passes 100%.
- **Resilience**: Locally, gates may skip specific tools with a warning if they are missing, but the CI pipeline is strict and will block on any failure.

## 2. Sandbox & Environment Constraints (macOS Seatbelt)

AI agents operate in a restricted sandbox. You MUST adhere to these path redirections and environment settings:

- **Operation not permitted**: If you encounter this error (or "Permission denied"), it is likely due to sandbox constraints. Do not attempt to work around these by modifying system paths.
- **Mandatory ./tmp usage**: All caching, builds, and transient artifacts MUST reside in the project-local `./tmp`.
- **Environment Variables**: Always prepend or export the following when running build or test tools:
  - `GOCACHE=$(pwd)/tmp/go-cache`
  - `GOLANGCI_LINT_CACHE=$(pwd)/tmp/lint-cache`
  - `TMPDIR=$(pwd)/tmp`
  - `HOME=$(pwd)/tmp/swift-home`
- **Swift Command Redirection**: When building or testing native components, always append:
  - `--disable-sandbox`
  - `--build-path ./tmp/swift-build`
- **Prefer Makefile**: Use namespaced `make` targets (e.g., `make go/build`, `make native/check`) as they are already configured to respect these sandbox-friendly paths.

---

## 3. GitHub Operations & API

To minimize redundancy, this project delegates foundational logic to the `agentic-core` extension:

- **Shell Safety**: Strictly follow the shell safety protocols defined by the **agentic-core** extension.
- **GitHub API**: Use the extension's `github-operations` skill for all GraphQL/Rest interactions.
- **Review Protocol**: Follow the extension's `reviewer-role` SIGN-OFF protocol.
