# Agent Guidelines: gh-orbit

## 1. Core Principles & Tech Stack

- **Local-First**: Prioritize local SQLite (`modernc.org/sqlite`, CGO-free) over API polling.
- **TUI-Centric**: Use `bubbletea`, `bubbles`, and `lipgloss` for all UI.
- **Zero-Config**: Use `go-gh/v2` for auth; no manual PATs.
- **Platform Native**: Follow XDG spec for persistence:
    - **Config**: `~/.config/gh/extensions/gh-orbit/`
    - **Data/DB/Logs**: `~/.local/state/gh-orbit/`
- **Secure**: NEVER commit secrets/tokens. Redact sensitive data from logs.

## 2. Development Workflow

### 2.1 Task Cycle
1. **Sync**: `git pull origin main`.
2. **Plan**: Mandatory [Strategy Review](.agent/workflows/strategy-review/WORKFLOW.md). **SIGN-OFF** required before `internal/` or `cmd/` changes.
3. **Branch**: `feat/` or `fix/`.
4. **Code**: Conventional commits. Include appropriate co-author attribution for the AI tool being used.
5. **Validate**: MANDATORY local check: `make fmt lint build test`. Run `go mod tidy` on dependency changes.
6. **PR**: `gh pr create --base main`. Address feedback.

### 2.2 Proactiveness & Agreements
- **Approvals**: Mandatory user agreement before any merge or destructive action.
- **Roadmap**: Refer to `.agent/implementation_plan.md`. Suggest next task on completion.
- **Clarity**: Explain intent before executing critical shell commands.

## 3. Testing Strategy

- Use the testify for asserting in test cases.
  - For assertions of prerequisites in test cases, use `require` so that the test fails if the prerequisite is not met immediately.
  - For assertions of expected results in test cases, use `assert` so that the test fails if the expected result is not met, but continue to run the test.
- Use assertions as much as possible to confirm that the test result is really expected. DON'T omit it without any explicit reason.
