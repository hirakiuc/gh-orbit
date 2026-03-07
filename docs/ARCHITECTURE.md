# Architecture: gh-orbit

This document describes the high-level design and engineering standards of `gh-orbit`.

## 1. Core Architecture (TEA + DI)

`gh-orbit` follows **The Elm Architecture (TEA)** via the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework. To ensure testability and decoupling, we employ strict **Interface-Based Dependency Injection**.

### TUI Model Decoupling

The TUI `Model` does not depend on concrete implementations of services. Instead, it interacts through specialized interfaces defined in `internal/types/api.go`:

- `Syncer`: Orchestrates notification fetching and state updates.
- `Alerter`: Manages system-level notifications (macOS/Native).
- `TrafficController`: Serializes and prioritizes API/DB access.
- `Repository`: Provides a unified interface for the SQLite persistence layer.

## 2. Lifecycle & Context Management

We adhere to the "Context-less Struct" mandate to prevent memory leaks and ensure clean propagation.

### Two-Phase Shutdown (Go 1.26+)

To guarantee that telemetry (OTel traces) and logs are flushed to disk even during forced termination, `main.go` implements a two-phase shutdown:

1. **Phase 1 (Cancellation)**: The application's root context is cancelled (via `SIGINT/SIGTERM`), signaling all background workers to stop.
2. **Phase 2 (Cleanup)**: A final cleanup sequence is executed using `context.WithoutCancel(ctx)`, ensuring that I/O operations for telemetry persist.

### Trace-Log Correlation

Every user action and background task is linked to the root `session` span. All log lines include a `trace_id`, enabling deep observability into the system's asynchronous operations.

## 3. Persistence & Security

`gh-orbit` implements a hardened persistence layer compliant with **XDG v0.8+** standards.

### The Discovery Ladder

To prevent data loss when environment variables change, the system uses a tiered discovery process:

1. Check `$XDG_DATA_HOME/gh-orbit/` (Modern Data path).
2. Check `$XDG_STATE_HOME/gh-orbit/` (Modern State/Log path).
3. Fallback to Legacy paths (e.g., `~/.config/gh/extensions/gh-orbit`).

**Atomic Migration**: When legacy data is found, the system performs an atomic "Stage-Swap" migration using SHA-256 verification to ensure 100% data integrity before removing legacy artifacts.

### Security Mandate (0700/0600)

All directories and files created by the application are secured with strict Unix permissions:

- **Directories**: `0700` (`drwx------`) - Private to the user.
- **Files**: `0600` (`-rw-------`) - Sensitive metadata (DB, Logs, Traces) is protected from other local users.

## 4. Testing Strategy

We employ a **Hybrid Testing Model** to balance speed with fidelity:

- **Service Logic**: Tested using [Mockery](https://github.com/vektra/mockery)-generated mocks for interfaces.
- **Data Persistence**: Tested against a **Real In-Memory SQLite** database (`modernc.org/sqlite`) to ensure SQL query accuracy without disk side-effects.
- **TUI Visuals**: Unit tests for the `Model.Update` loop and rendering components using `testify/assert`.

---

## 5. Development Principles

- **Doc-First**: Run `go doc` before implementing external library calls.
- **Fail-Fast**: Configuration errors and permission failures are reported immediately on startup.
- **Context Awareness**: `ctx context.Context` is always the first parameter of any I/O or background method.
