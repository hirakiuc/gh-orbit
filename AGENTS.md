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

To minimize coordination overhead, agents use three static file paths for task state:

- **Active Context**: `.agents/issue.md` (Cache for target GitHub Issue description).
- **Active Proposal**: `.agents/proposal.md` (Live design workbench).
- **Active Feedback**: `.agents/feedback.md` (Live audit log from the Reviewer).
- **Optional RFC**: `.agents/rfc.md` (Persistent log for high-level architectural discussion).

### 1.2 Task Cycle

1. **Sync**: `git pull origin main` before starting any task.
2. **Triage**: Run `make roadmap` to establish the current project state and pick the next target.
3. **Initialize**: Run `make task ID="<issue-id>"` to populate the workbench.
4. **Strategy Review**: Follow the **Hybrid Loop** (RFC vs Proposal) before implementation.
5. **Implement**: Create a topic branch via `gh issue develop <ID>`.
6. **Validate**: Run `make check` (linting + tests) after every major change.
7. **Submit**: Create a PR using `gh pr create`. Reference the Issue ID.
8. **Cleanup**: Run `make reset-task` after the PR is merged.

### 1.3 Branch Persistence

- **Stay on Branch**: NEVER switch away from a topic branch (e.g., to `main`) until you have received an explicit **SIGN-OFF** in `.agents/feedback.md` or a direct user instruction.
- **Completion**: A task is complete ONLY when the PR is merged or the user directs you to move to a new task.

---

## 2. Role Boundaries & Interaction

### 2.1 Role Definitions

- **Worker**: Responsible for implementation, testing, and PR submission. Operates in Section 4 (Implementation) of the Proposal.
- **Reviewer**: Responsible for auditing proposals and implementation. Must operate in **Read-Only** mode for source files.
- **Manager (User)**: Orchestrates the handoff between roles and provides final sign-off.

### 2.2 Hybrid Loop (RFC vs Proposal)

**RFC Path (High Uncertainty)**:

- **Triggers**:
  - Any change introducing a new external dependency.
  - Any architectural shift touching `internal/db` or core interfaces.
  - Any task estimated to touch >5 files or having a high "Blast Radius".
- **Action**: Share a high-level strategy summary or use `.agents/rfc.md` before formalizing.

**Proposal Path (Refined Implementation)**:

- **Triggers**: Features, bug fixes, or well-defined patterns following successful RFC alignment.
- **Action**: Draft the formal `.agents/proposal.md` and increment Revision numbers based on feedback.

### 2.3 SIGN-OFF Protocol

- **Strict Prohibition**: The `SIGN-OFF` marker MUST NOT be included if there are any "Required Fixes" or "Critical/Blocking" findings.
- **Conditional Allowance**: `SIGN-OFF` is allowed if only "Suggestions" or "Non-Blocking" improvements remain.
- **Machine-Readable Format**: To ensure automated processing, the `SIGN-OFF` marker MUST be placed on its own line in a "Final Decision" section at the end of the report in `.agents/feedback.md`.

    Example:

    ```markdown
    ## Final Decision
    SIGN-OFF
    ```

### 2.4 Adopt vs Defer Protocol

When receiving a **SIGN-OFF** with suggestions:

- **Adopt**: If the suggestion is in-scope, implement it. This requires a final **Implementation Audit** by the Reviewer.
- **Defer**: If out-of-scope, create a new GitHub Issue before merging.

---

## 3. Sandbox & Environment Constraints (macOS Seatbelt)

AI agents operate in a restricted sandbox. You MUST adhere to these path redirections:

- **Mandatory ./tmp usage**: All caching and build artifacts MUST reside in the project-local `./tmp`.
- **Environment Variables**: Always prepend the following to shell commands:
  - `GOCACHE=$(pwd)/tmp/go-cache`
  - `GOLANGCI_LINT_CACHE=$(pwd)/tmp/lint-cache`
  - `TMPDIR=$(pwd)/tmp`
- **Prefer Makefile**: Use `make build`, `make test`, `make lint` as they are pre-configured with these paths.
- **Self-Correction**: If you encounter "Operation not permitted" or "Permission denied," it is a signal to check your environment variable redirections and project-local paths before escalating to the user.

---

## 4. Implementation Rules

### 4.1 Reliability & Precision

- **API Verification**: Run `go doc <package>.<symbol>` before calling external libraries to ensure signature accuracy.
- **Surgical Refactoring**: For files >200 lines (e.g., `internal/tui/update.go`), use `replace` or `insert` instead of `write_file` to prevent logic erasure.
- **Impact Analysis**: Use Serena's `find_referencing_symbols` before modifying core interfaces.
- **Context Hygiene**: Never store `context.Context` in structs. Pass it as the first argument.

### 4.2 GitHub Operations & GraphQL

- **Precise Typing**: Use `-F` for numbers/booleans and `-f` for strings in `gh api graphql`.
- **Thread Management**: Use the specific mutations `addPullRequestReviewThreadReply` and `resolveReviewThread` (NOT `resolvePullRequestReviewThread`).
- **Shell Safety (High-Precision Patterns)**:
  - **Piping Multi-line Content**: Use `jq -nr --arg body "$BODY" '$body' | gh ... --body-file -` to safely handle arbitrary content including newlines and special characters.
  - **Variable Injection**: Avoid raw variable expansion in shell strings. Prefer `printf "%s" "$VAR" | ...` or `jq` argument passing.

### 4.3 Shell Safety Standards

- **Prefer Single Quotes**: For all static command arguments.
- **Heredocs**: Use `cat << 'EOF'` (quoted EOF) for multi-line text to disable expansion.
- **Validation**: Perform a dry run (e.g., `echo "..."`) of complex commands involving dynamic strings before final execution.

---

## 5. Audit Protocol (Universal)

All reviews must follow these severity levels:

- **CRITICAL**: Blocking finding (must fix before sign-off).
- **WARN**: Best practice violation (should fix).
- **INFO**: Style suggestion (optional).

Unified Output Format: `[TAG-SEVERITY] <File Path>[:Line]: <Description>`
