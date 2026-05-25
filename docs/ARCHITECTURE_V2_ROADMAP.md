# Architecture V2 Roadmap

This document turns [ARCHITECTURE_V2.md](ARCHITECTURE_V2.md) into an execution roadmap.

It defines:

- the recommended migration order
- the first-wave follow-up issue breakdown
- preserved invariants per slice
- validation expectations per slice
- explicit transport-related deferrals

This is an incremental roadmap, not a rewrite plan.

## 1. Planning objective

The purpose of this roadmap is to make `ARCHITECTURE_V2` executable without another planning pass.

The roadmap should answer:

- what to do first
- what should be split into separate issues
- what must remain true while each slice lands
- what must be deferred until backend cleanup is complete

## 2. Ordered migration tracks

Realize `ARCHITECTURE_V2` in this order:

1. establish the backend seam
2. move the TUI onto that seam
3. demote transport and mode-specific behavior behind it

This is the recommended order because it creates the highest-leverage boundary first and avoids rewriting transport before a transport-agnostic backend surface exists.

## 3. Recommended execution sequence

## Step 1: introduce a narrow backend client surface

Recommended starting point: start here first.

- Why first:
  - every later slice benefits from a cleaner application-facing seam
  - it reduces the chance that UI, repository, and transport concerns keep bleeding together
  - it is the smallest coherent slice that unlocks the rest of the roadmap
- Primary seams:
  - `internal/tui`
  - `internal/types.NotificationStore`
  - TUI-facing portions of `internal/engine/adapter.go`
  - orchestration entry points above `internal/api`
- Outcome:
  - one narrow backend-facing client contract for operations such as list, sync, mark read, set priority, fetch detail, and refresh/event subscription
- Preserved invariant:
  - embedded and standalone TUI behavior remains semantically identical
- Minimum validation:
  - targeted `go test` for touched packages
  - `make go/check`

## Step 2: formalize backend orchestration above current services

- Why second:
  - once the client seam exists, the backend needs a single orchestration owner rather than UI- or transport-shaped call paths
- Primary seams:
  - `internal/api`
  - `internal/engine/engine.go`
  - selected service composition around `internal/db` and `internal/github`
- Outcome:
  - one orchestration layer owns mutation semantics, sync/detail composition, and event publication
- Preserved invariant:
  - SQLite remains the local source of truth and current sync/enrichment behavior stays functionally intact
- Minimum validation:
  - targeted `go test` for touched packages
  - `make go/check`

## Step 3: migrate TUI mutation paths to backend operations

- Why third:
  - after the seam and orchestration layer exist, the TUI can stop depending on persistence-shaped mutation calls directly
- Primary seams:
  - `internal/tui/actions.go`
  - `internal/tui/update.go`
  - `internal/tui/model.go`
- Outcome:
  - TUI issues backend operations rather than local mutation-policy calls
- Preserved invariant:
  - current TUI read-state and priority flows keep the same user-visible outcomes and refresh behavior
- Minimum validation:
  - targeted `go test` for `internal/tui` and adjacent touched packages
  - `make go/check`

## Step 4: unify standalone and connected TUI semantics

- Why fourth:
  - once the TUI uses the backend seam, startup modes can be reduced to transport/bootstrap differences rather than business-rule differences
- Primary seams:
  - `cmd/gh-orbit/main.go`
  - connected vs standalone assumptions in `internal/tui`
- Outcome:
  - only startup/transport differs; config and mutation semantics do not
- Preserved invariant:
  - backend remains sole authority for persisted mutation outcomes and signaling
- Minimum validation:
  - targeted package tests
  - `make go/check`
  - broaden to `make check` if native integration or cross-language paths are touched

## Step 5: narrow `MCPAdapter` to the backend client contract

- Why fifth:
  - transport can only be narrowed cleanly after the backend seam exists and the TUI uses it
- Primary seams:
  - `internal/engine/adapter.go`
  - transport-facing interface assertions and tests
- Outcome:
  - adapter becomes a clear client transport layer instead of a broad pseudo-runtime facade
- Preserved invariant:
  - MCP-connected consumers still observe the same refresh/event contract
- Minimum validation:
  - targeted package tests
  - `make go/check`

## Step 6: refactor MCP server handlers to pure transport delegation

- Why sixth:
  - server handlers should stop encoding behavioral policy once backend orchestration is authoritative
- Primary seams:
  - `internal/engine/mcp.go`
  - event/resource signaling around `internal/engine/events.go`
- Outcome:
  - MCP server logic becomes protocol translation and event bridging over backend use cases
- Preserved invariant:
  - current MCP tools/resources remain functional for existing clients
- Minimum validation:
  - targeted engine/MCP tests
  - `make go/check`

## Step 7: consolidate runtime ownership and config authority

- Why seventh:
  - remaining cleanup becomes easier once UI and transport no longer distort ownership boundaries
- Primary seams:
  - `internal/engine/engine.go`
  - constructors across `internal/api`
  - startup composition in `cmd/gh-orbit/main.go`
- Outcome:
  - one obvious runtime owner and one authoritative config flow
- Preserved invariant:
  - no subsystem regains separate config authority or shared-service shutdown ownership
- Minimum validation:
  - targeted package tests
  - `make go/check`
  - `make check` if the wiring crosses native integration boundaries

## Step 8: re-evaluate transport strategy

- Why last:
  - only now is there a clean enough backend surface to judge `MCP` vs `gRPC` vs `MCP+gRPC`
- Primary seams:
  - design/documentation only at first
- Outcome:
  - a transport decision record based on a cleaned architecture instead of current-state ambiguity
- Preserved invariant:
  - transport remains secondary to backend architecture
- Minimum validation:
  - `make go/lint-docs`

Completed decision record:

- [TRANSPORT_DECISION.md](TRANSPORT_DECISION.md)

## 4. First-wave issue breakdown

These should become the first follow-up issues.

### Issue 1: Introduce backend client interface for TUI-facing operations

- Scope:
  - define the narrow application-facing API and initial implementation path
- Preserved invariant:
  - embedded and standalone TUI behavior remains semantically identical
- Validation:
  - TUI can target the new boundary in tests
  - `make go/check`

### Issue 2: Create backend orchestration layer above current `internal/api` services

- Scope:
  - formalize orchestration ownership for sync, mutation semantics, detail fetch, and event publication
- Preserved invariant:
  - SQLite remains the local source of truth and current sync/enrichment behavior stays intact
- Validation:
  - existing backend behavior remains green under the new owner
  - `make go/check`

### Issue 3: Migrate TUI read-state and priority actions to backend operations

- Scope:
  - remove direct persistence-shaped mutation assumptions from TUI flows
- Preserved invariant:
  - current read-state and priority semantics stay intact
- Validation:
  - targeted TUI behavior tests remain green
  - `make go/check`

### Issue 4: Unify standalone and connected TUI behavior around one backend model

- Scope:
  - eliminate meaningful business-rule divergence while preserving startup flexibility
- Preserved invariant:
  - backend remains the sole authority for persisted mutation outcomes and signaling
- Validation:
  - standalone and embedded/connected TUI behavior stays aligned
  - `make go/check`
  - `make check` if native-facing integration is touched

### Issue 5: Narrow `MCPAdapter` to the backend client contract

- Scope:
  - remove broad or misleading interface obligations
- Preserved invariant:
  - MCP-connected consumers still receive expected refresh behavior
- Validation:
  - adapter tests remain green under the narrowed contract
  - `make go/check`

### Issue 6: Refactor MCP server handlers to delegate to backend use cases

- Scope:
  - separate protocol handling from behavior ownership
- Preserved invariant:
  - existing MCP tools/resources stay functional
- Validation:
  - MCP server tests remain green
  - `make go/check`

### Issue 7: Consolidate runtime ownership and config authority

- Scope:
  - finish shared-service and config-authority cleanup
- Preserved invariant:
  - one authoritative config path and runtime owner remain obvious
- Validation:
  - ownership/config tests remain green
  - `make go/check`
  - `make check` if cross-language integration changes

### Issue 8: Evaluate post-cleanup transport strategy

- Scope:
  - design decision comparing MCP-only, MCP+gRPC, or gRPC-primary
- Preserved invariant:
  - the decision is based on cleaned backend architecture, not current ambiguity
- Validation:
  - resulting design record passes docs lint
  - `make go/lint-docs`

## 5. Grouping rules

Keep these as separate issues:

- backend client seam
- backend orchestration layer
- TUI mutation migration
- standalone/connected semantic unification
- MCP adapter narrowing
- MCP server delegation
- transport strategy reevaluation

This work may be grouped if it stays small in practice:

- runtime ownership cleanup with config-authority cleanup

Reasoning:

- the first six slices each change a distinct architecture seam and deserve isolated review/sign-off
- runtime ownership/config consolidation may collapse naturally once earlier seams are cleaner

## 6. Explicit deferrals

Do not schedule these before backend cleanup:

- replacing MCP with gRPC
- making gRPC the primary app-facing transport
- large transport-schema rewrites
- deleting MCP compatibility paths

These changes have poor leverage until the backend seam is transport-agnostic.

## 7. Follow-up issue template guidance

Each follow-up issue should include:

- the seam being changed
- the preserved invariant
- affected package list
- minimum validation commands
- a note if cross-language validation or MCP-observable behavior must be checked

## 8. Status

This roadmap is the planning artifact for issue `#356` and should be used to create the follow-up implementation issues for `ARCHITECTURE_V2`.
