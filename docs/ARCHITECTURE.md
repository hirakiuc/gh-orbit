# Architecture

This document describes the architecture implemented by the current `gh-orbit`
codebase. Git history and closed issues preserve completed migration plans and
decision chronology; this file is the durable reference for current behavior.

## System invariants

- SQLite is the local source of truth for notifications and local triage state.
- The backend owns persisted mutations and publishes their observable effects.
- The Bubble Tea TUI is a first-class interface in a terminal or inside Orbit
  Cockpit.
- UI clients own only ephemeral presentation state, such as focus, filters,
  cursors, selections, and pane layout.
- Transport adapts backend operations; it does not define business behavior.
- Local data and inter-process communication are private to the current user.

## Runtime topology

`gh-orbit` supports one backend model through three product surfaces.

### Standalone TUI

Running `gh orbit` first looks for the headless engine socket. When no usable
engine is available, the process creates a `CoreEngine` in-process and injects
its `AppBackend` into the TUI. The same process then owns SQLite, GitHub access,
sync, enrichment, traffic control, alerting, and shutdown.

### Connected TUI and headless engine

`gh orbit engine` creates a `CoreEngine` and exposes it through an MCP server on
a Unix domain socket. A TUI that discovers the socket initializes an MCP client,
checks that the engine exposes the required independent mutation tools, and
uses `MCPAdapter` as its backend.

Connected mode treats traffic control, sync, persistence, and mutation
reconciliation as engine-owned. If `GH_ORBIT_REQUIRE_ENGINE=1`, a missing or
incompatible engine is fatal instead of triggering standalone fallback. Orbit
Cockpit uses this requirement so its embedded TUI cannot silently establish a
second backend authority.

### Orbit Cockpit

The native macOS application is a host around the same Go engine. Its
`NativeEngineManager` reuses a compatible engine or starts the bundled helper,
then verifies readiness with an MCP initialize exchange. SwiftUI supplies
windowing, settings, review-workspace orchestration, logs, and terminal panes.
The terminal renderer is abstracted by `OrbitTerminalEngine`, with SwiftTerm as
the current implementation.

Component-specific native behavior remains documented under
`native/OrbitCockpit/`.

```text
regular terminal                         Orbit Cockpit
      |                                  /           \
 Bubble Tea TUI                  embedded TUI    native host UI
      |                                  |        (supervision,
      +------------ TUIBackend ----------+         terminals,
                    |                              orchestration)
          +---------+---------+                         |
          |                   |                         |
     AppBackend       MCPAdapter / MCP             starts or reuses
     (standalone)             |                         |
          |              CoreEngine <------------------+
          +---------+---------+
                    |
          SQLite + GitHub + services
```

The diagram shows equivalent backend contracts, not simultaneous ownership:
standalone mode uses an in-process `AppBackend`; connected clients use
`MCPAdapter` to reach the engine-owned `AppBackend`.

## Backend ownership

`internal/engine.CoreEngine` is the composition root for shared backend
services. Construction establishes:

1. the SQLite repository;
2. the GitHub client inherited from the authenticated `gh` environment;
3. the internal event bus and mutation publication hooks;
4. API traffic control and rate-limit reporting;
5. enrichment and alert services;
6. notification fetching and synchronization; and
7. the authoritative `internal/api.AppBackend`.

Shutdown stops sync, enrichment, traffic control, and alerts before closing the
database.

The application-facing contract is `types.TUIBackend`. It includes notification
listing and detail retrieval, sync, independent read and handled-state changes,
priority changes, batch operations, and review-workspace requests. The TUI
depends on this contract rather than SQLite or GitHub implementations.

### Mutation semantics

Local persistence is authoritative for UI-visible triage state. Backend methods
perform local mutations, publish notification-change events, and reconcile any
required GitHub request. Read and handled states are independent: handled state
is local-only, while marking a notification read also requests the corresponding
GitHub change. GitHub does not expose an arbitrary mark-unread operation, so
unread changes remain local.

Batch mutations use the same backend semantics as single-item mutations. The
backend prepares local state transactionally, performs bounded remote read
requests where applicable, and returns an authoritative snapshot or explicit
reconciliation state to the caller.

## Events and MCP

The internal `EventBus` publishes coalescing, non-blocking signals for changes
to the notification list and enrichment data. Backend mutation hooks and sync
or enrichment services emit these signals; publishers never wait for a slow
subscriber.

The MCP server adapts the backend into tools and resources. It subscribes to
engine events and emits resource-update notifications so connected clients can
refresh from authoritative state. The MCP adapter translates TUI backend calls
without introducing separate mutation rules.

## Transport and security

MCP over a local Unix domain socket is the only implemented inter-process
transport. It remains a good fit for the current local-first, single-user
product and retains tool- and agent-oriented interoperability without a second
RPC stack.

The engine creates its runtime directory with mode `0700` and its socket with
mode `0600`. On macOS, accepted connections must come from the same user and,
unless the explicit insecure development option is enabled, from an allowed
code-signed gh-orbit identity. The native host and CLI resolve the same
`gh-orbit/engine.sock` runtime path.

The backend contract remains transport-agnostic. Re-evaluate an additional
gRPC adapter if one or more of these conditions become material:

- Swift development is repeatedly constrained by weak typed-client ergonomics;
- event or streaming adaptation becomes persistently complex;
- multiple rich local clients require conventional app-oriented RPC;
- MCP interoperability no longer justifies its maintenance cost; or
- transport code begins accumulating business behavior.

If that pressure appears, evaluate MCP plus gRPC before replacing MCP outright.

## Persistence, configuration, and observability

The database uses `modernc.org/sqlite` with WAL mode, foreign keys, a busy
timeout, and immediate transaction locking. Data follows XDG paths, with
legacy-data migration where supported. Private directories use mode `0700` and
private files use `0600`; startup permission audits repair safe, user-owned
paths when possible.

Configuration is loaded at process composition and passed into the services it
owns. Subsystems do not establish competing configuration authority.

Every command initializes structured logging. Verbose mode also initializes
OpenTelemetry and creates a session span carrying version, operating-system,
and architecture attributes. Runtime contexts and explicit shutdown methods
bound background work and resource cleanup.

## Package responsibilities

| Package | Responsibility |
| --- | --- |
| `cmd/gh-orbit` | CLI composition, mode selection, lifecycle, and diagnostics |
| `internal/api` | Application services and authoritative backend use cases |
| `internal/engine` | Backend composition, events, MCP server, and MCP client adapter |
| `internal/db` | SQLite persistence, migrations, and local state transactions |
| `internal/github` | GitHub API integration and notification fetching |
| `internal/config` | Configuration, XDG paths, logging, telemetry, and permissions |
| `internal/triage` | Pure triage policy and notification classification |
| `internal/tui` | Bubble Tea presentation and ephemeral interaction state |
| `internal/types` | Shared application contracts and value types |
| `native/OrbitCockpit` | SwiftUI host, engine supervision, terminal panes, and orchestration |

Dependencies flow from presentation and transport adapters toward application
contracts. Business behavior belongs in application or domain services, not in
the TUI, native renderer, or MCP handlers.

## Validation and testing

Go services use unit tests with Testify and generated Mockery test doubles;
persistence tests exercise SQLite behavior. TUI tests cover update and rendering
semantics. Orbit Cockpit uses Swift Testing for native lifecycle, settings, and
terminal-host behavior.

Run `make check` for the complete Go and native quality gate. Run
`make quality-report` for an on-demand complexity scorecard under
`tmp/quality-report.md`; mutation testing remains an explicit manual diagnostic
through `make quality-mutation`.
