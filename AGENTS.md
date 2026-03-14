# Agent Guidelines: gh-orbit

## 1. Core Principles & Tech Stack

- **Local-First**: Prioritize local SQLite (`modernc.org/sqlite`, CGO-free) over API polling.
- **TUI-Centric**: Use `bubbletea`, `bubbles`, and `lipgloss` for all user interactions.
- **Observability**: Every action must be traced via `go.opentelemetry.io/otel`.
- **Zero-Config**: Credentials should be inherited from the `gh` host environment.

## 2. Standard Operating Procedures (SOPs)

### 2.1 Task Cycle

1. **Sync**: `git pull origin main` before starting any task.
2. **Triage**: Run `make roadmap` to establish the current project state and pick the next target.
3. **Verify**: Run `make build test lint` to establish a baseline.
4. **Implement**: Follow the Strategy Review Workflow in `.agent/workflows/strategy-review/WORKFLOW.md`.
5. **Validate**: Run `make generate test lint` after every major change.
5. **Audit**: Run `gh orbit doctor` to verify environment health.

### 2.2 Proactiveness & Agreements

- **Approvals**: Mandatory use of `ask_user` for:
  - Destructive operations (e.g., clearing local database).
  - Strategic changes that deviate from the Implementation Plan.
  - Adding new external dependencies.
- **Attribution**: All commits must include:
    `Co-authored-by: Gemini CLI <gemini-cli+noreply@google.com>`

## 3. Implementation Patterns

- **Dependency Injection**: Always use interface-based DI for service orchestration.
- **Context Hygiene**: Contexts must NEVER be stored in structs. Pass `ctx context.Context` as the first argument.
- **Hardened SQLite**: Use WAL mode and foreign keys for all local storage.
- **Testing**: Use the `testify` for asserting in test cases.
  - For assertions of prerequisites in test cases, use `require` so that the test stops if the prerequisite is not met.
  - For assertions of expected results in test cases, use `assert` so that the test fails if the expected result is not met, but continue to run the test.
- Use assertions as much as possible to confirm that the test result is really expected. DON'T omit it without any explicit reason.

## 4. Reliability & Precision Rules

- **API Verification**: Always run `go doc <package>.<symbol>` before implementing calls to external libraries (especially `v2+` versions) to ensure 100% signature and behavior accuracy.
- **Surgical Refactoring**: For large controller or logic files (>200 lines, e.g., `update.go`), prioritize the use of `replace` or `insert_after_symbol` instead of `write_file` to prevent accidental feature regressions (logic erasure).
- **Mock Synchronicity**: When modifying interfaces, run `make generate` immediately after the interface change and before fixing implementations or tests to maintain a consistent build state.
- **Impact Analysis**: Before modifying a core interface or symbol, use Serena MCP's `find_referencing_symbols` to map the blast radius. This ensures that tests and dependent logic are updated in the same turn, preventing build-fail loops.
- **Feature Preservation**: Before refactoring complex logic paths, explicitly list the features being touched and verify their parity after the change.
