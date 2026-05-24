# Architecture V2 Proposal

This document records the approved architecture direction from issue `#354`:

- unify backend authority and ownership
- preserve the TUI as a first-class product surface
- preserve embedded TUI support inside the native app
- treat transport as an adapter boundary rather than the shape of the system

This is a target architecture and migration document, not a big-bang rewrite plan.

For the approved implementation sequencing and issue breakdown, see [ARCHITECTURE_V2_ROADMAP.md](ARCHITECTURE_V2_ROADMAP.md).

## 1. Why a v2 architecture

The current system is functional, but recent work has repeatedly tightened boundaries that were previously ambiguous:

- standalone vs connected mode divergence
- shared-service ownership split across multiple layers
- configuration authority drift
- transport concerns shaping internal behavior too directly
- interface surfaces that are broader than the real caller needs

That pattern suggests the current project is stable enough to use, but still paying an architectural tax. This document defines the intended long-term shape before more runtime paths accumulate.

## 2. Hard constraints

Any v2 architecture must preserve these constraints:

- the native app must support the TUI in an embedded terminal view
- SQLite remains the local source of truth
- the TUI remains a first-class product surface
- the backend is the sole authority for persisted state mutations
- transport remains a boundary concern, not the core internal abstraction

## 3. Recommended runtime model

`gh-orbit` should converge on an engine-first runtime model:

- one backend runtime owns SQLite, sync, enrichment, alerts, mutation semantics, and event publication
- the TUI becomes a client of that backend whether it runs in a terminal or inside the native app
- the native app becomes a host around the same backend runtime, combining native views and embedded TUI sessions against shared state

This intentionally rejects two alternatives:

- `TUI-first ownership`: rejected because the native app must consume the same behavior while embedding the TUI
- `parallel frontends with duplicated business logic`: rejected because it preserves divergence and ownership ambiguity

## 4. Target topology

### 4.1 Backend authority

One runtime owns all shared mutable behavior:

- config authority
- SQLite access
- GitHub integration
- sync scheduling
- enrichment
- alerting
- persisted mutation rules
- event publication

No UI surface should define a separate persisted-state rule set.

### 4.2 Client surfaces

The client surfaces are:

- TUI in a regular terminal session
- TUI embedded in the native app terminal view
- native app dashboards and orchestration panels

These clients may keep local ephemeral UI state such as:

- selection
- focus
- cursor position
- pane layout
- transient loading indicators

They must not own separate mutation semantics or config authority.

### 4.3 Transport

Transport adapters connect clients to the backend runtime.

Current transport:

- MCP over local UDS

Future transport:

- MCP may remain sufficient
- gRPC may later be added or adopted for app-facing transport

That decision is explicitly deferred until the backend boundary is cleaner. The backend should first expose a transport-agnostic application surface.

## 5. Layer model

The intended layering is:

1. `domain/core`
   - pure notification and state-transition logic
   - no transport, no persistence, no UI
2. `application/backend`
   - use cases such as sync, mark read, set priority, fetch detail
   - event emission
   - authoritative mutation semantics
3. `persistence/integration`
   - SQLite adapter
   - GitHub adapter
4. `runtime/engine`
   - lifecycle host
   - shared-service ownership
   - transport wiring
5. `presentation`
   - TUI
   - native app
6. `transport adapters`
   - MCP now
   - possible gRPC later

Rule: transport adapters adapt backend use cases. Backend use cases must not be primarily shaped by transport semantics.

## 6. Current-to-target mapping

| Current seam | Current role | Target role | Direction |
| --- | --- | --- | --- |
| `internal/tui` | UI plus some mutation/reconciliation behavior | presentation-only client over backend operations | narrow dependencies away from persistence-shaped calls |
| `internal/api` | mixed application services | application/backend orchestration | preserve service logic and formalize a cleaner backend surface above it |
| `internal/db` | SQLite repository and local mutation storage | persistence adapter | preserve as local source of truth |
| `internal/github` | GitHub API transport | integration adapter | preserve as GitHub-only boundary |
| `internal/engine/engine.go` | runtime wiring and ownership | runtime/engine host | preserve single-owner role and simplify public shape |
| `internal/engine/adapter.go` | MCP-backed client proxy plus broad interface surface | client transport adapter | narrow to backend-client semantics |
| `internal/engine/mcp.go` | MCP server registration and direct mutation/resource signaling | MCP transport adapter | delegate behavior to backend use cases |
| `cmd/gh-orbit/main.go` | startup and mode selection | composition/bootstrap only | reduce meaningful runtime divergence |

## 7. Explicit architecture decisions

### 7.1 Backend owns persisted mutations

Persisted state mutations must be executed by backend/application services.

Examples:

- mark read
- set priority
- refresh/reload rules after mutation
- event publication for observers

The TUI and native app may initiate these actions, but they should not define the final semantics themselves.

### 7.2 Config is loaded once per process

Config authority flows from one top-level load path downward. Subsystems must not establish their own authoritative reload behavior.

### 7.3 TUI remains canonical as terminal UI

Bubbletea remains the canonical terminal experience. The embedded TUI in the native app should behave like the standalone TUI, not like a separate feature fork.

### 7.4 Native app is a host, not a backend owner

The native app remains a first-class surface, but it should host or consume the backend runtime rather than directly owning backend subsystems.

### 7.5 MCP stays for now, but as an adapter

MCP remains the current transport boundary. The v2 direction is not “remove MCP now.” The direction is “stop letting MCP define the internal system shape.”

## 8. First implementation slices

These are the first follow-up slices after this design document is approved.

### Slice 1: introduce a narrow backend client surface for TUI-facing operations

- Seam to change:
  - `internal/tui`
  - `internal/types.NotificationStore`
  - MCP-backed TUI calls in `internal/engine/adapter.go`
- Intended owner:
  - application/backend surface, likely formalized above the existing `internal/api` services
- Goal:
  - replace persistence-shaped UI dependencies with backend operations such as sync, mark read, set priority, list notifications, and fetch detail
- Preserved invariant:
  - embedded TUI and standalone TUI continue to present the same user-visible behavior

Recommended starting point: start here first. It creates the cleanest seam for every later slice and reduces the chance that transport or repository shapes continue to leak into UI behavior.

### Slice 2: remove meaningful standalone vs connected mutation-authority divergence

- Seam to change:
  - launch/wiring split in `cmd/gh-orbit/main.go`
  - connected-vs-standalone behavior assumptions in `internal/tui`
- Intended owner:
  - runtime/engine plus TUI integration boundary
- Goal:
  - allow different startup paths, but unify config authority, mutation semantics, and refresh/event behavior under the same backend model
- Preserved invariant:
  - backend remains the sole authority for persisted mutation outcomes and refresh signaling

### Slice 3: demote MCP from behavioral seam to transport adapter

- Seam to change:
  - `internal/engine/mcp.go`
  - `internal/engine/adapter.go`
- Intended owner:
  - runtime transport layer adapting backend use cases
- Goal:
  - keep current MCP compatibility while making transport handlers delegate into backend operations instead of shaping them
- Preserved invariant:
  - MCP-connected consumers, including native app integrations, continue to receive the required observable refresh/event behavior

## 9. Migration phases

The migration should remain incremental.

### Phase 1: declare target topology

- document engine-first ownership
- document config, DB, eventing, sync, enrichment, alerting, and UI authority rules
- align future issues to this document

### Phase 2: define backend application surface

- formalize the narrow backend use-case API
- make UI clients depend on backend operations instead of persistence-shaped seams

### Phase 3: reduce mode divergence

- remove meaningful standalone vs connected behavioral differences
- keep startup differences only where necessary

### Phase 4: isolate transport adapters

- move MCP-specific concerns behind backend use cases
- ensure transport mirrors behavior rather than defines it

### Phase 5: re-evaluate transport

- decide whether MCP remains enough
- or whether app-facing transport should add or adopt gRPC

## 10. Transport decision after cleanup

Transport strategy is a second-order decision after backend cleanup.

The decision record should compare:

- keep MCP only
- add gRPC as the primary app-facing transport while retaining MCP compatibility
- eventually migrate app-facing transport to gRPC

This document does not choose among those options yet. It only establishes that the backend boundary must become transport-agnostic first.

## 11. Non-goals

This v2 architecture does not imply:

- a big-bang rewrite
- immediate package renames
- immediate MCP replacement
- deleting the TUI in favor of native-only UI
- duplicating backend behavior in the native app

## 12. Approval criteria for follow-up issues

Follow-up issues derived from this document should preserve at least one explicit invariant from their parent slice, for example:

- embedded TUI still works
- standalone and connected TUI behavior does not diverge
- backend remains sole mutation authority
- MCP-connected consumers still observe refresh events correctly

## 13. Status

This document reflects the approved design direction from issue `#354`. It should be used as the reference architecture note for future boundary-cleanup issues until superseded by a later design record.
