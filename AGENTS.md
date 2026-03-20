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
3. **Verify**: Run `make check` to establish a baseline.
4. **Implement**: Follow the Strategy Review Workflow in `.agents/workflows/strategy-review/WORKFLOW.md`.
5. **Validate**: Run `make generate && make check` after every major change.
6. **Audit**: Run `gh orbit doctor` to verify environment health.

### 2.2 Proactiveness & Agreements

- **Approvals**: Mandatory use of `ask_user` for:
  - Destructive operations (e.g., clearing local database).
  - Strategic changes that deviate from the Implementation Plan.
  - Adding new external dependencies.
- Attribution: All commits must include:
    `Co-authored-by: Gemini CLI <gemini-cli+noreply@google.com>`

### 2.3 Branch Persistence

- **Stay on Branch**: NEVER switch away from a topic branch (e.g., to `main`) until you have received an explicit **SIGN-OFF** in `.agents/feedback.md` or a direct user instruction to do so. This ensures you remain in the correct context for iterating on reviewer feedback.
- Completion: A task is only considered complete when the pull request is merged or the user directs you to move to a new task.

### 2.4 Role Boundaries

- **Worker**: Responsible for implementation, testing, and modifying source files. Follows the Task Cycle and Strategy Review Workflow.
- **Reviewer**: Responsible for auditing proposals and implementation. Must operate in **Read-Only** mode relative to source files. The only file a Reviewer should modify is `.agents/feedback.md`.

## 3. Sandbox & Environment Constraints

This project enforces a restricted sandbox for AI agents (e.g., via macOS Seatbelt).

- **Mandatory ./tmp usage**: All caching, build artifacts, and transient files MUST reside in the project-local `./tmp` directory.
- **Environment Redirection**: When executing shell commands, you must ensure that tool-specific caches are redirected:
  - **Go**: `GOCACHE=$(pwd)/tmp/go-cache`
  - **Linters**: `GOLANGCI_LINT_CACHE=$(pwd)/tmp/lint-cache`
  - **System Tmp**: `TMPDIR=$(pwd)/tmp`
- **Rationale**: This ensures that agent file modifications are isolated to the project's boundary and remain compliant with global security policies.
- **Troubleshooting**: If you encounter "Operation not permitted" or "Permission denied" when running a shell command, it is a signal that you are attempting an action outside the sandbox. Adjust your command to use project-local paths or consult the `Makefile` for pre-configured targets.

## 4. Implementation Patterns

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
