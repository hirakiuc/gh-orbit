# Internal Overall Design Refinement

This document records the approved planning direction from issue `#389`.

It defines the next internal-quality track after:

- `ARCHITECTURE_V2`
- the v2 implementation roadmap
- the post-v2 internal improvement sequence in issues `#376` through `#381`

This is a planning document, not an implementation change.

## 1. Purpose

The goal of this track is to improve the **internal overall design** of the current codebase before more complex native-facing features put additional pressure on it.

At this point, the main problem is no longer broad architectural ambiguity. The major runtime model is substantially cleaner:

- backend authority is explicit
- TUI/backend/transport boundaries are narrower
- runtime ownership and config authority are clearer
- transport is more consistently treated as an adapter

The remaining risk is different:

- local surprise inside dense subsystems
- composition surfaces that are still harder to reason about than they should be
- mechanically correct code that remains expensive to extend confidently

This plan focuses on those internal design pressures.

## 2. What This Track Is And Is Not

### 2.1 This track is

- incremental
- codebase-grounded
- surgical where possible
- focused on reducing local complexity and future surprise

### 2.2 This track is not

- another architecture-v2 rewrite
- another transport-strategy decision
- a broad package shuffle for cosmetic alignment
- a concurrency redesign without stronger evidence

Rule: if a proposed improvement primarily changes ownership, runtime mode, transport strategy, or backend authority boundaries, it belongs outside this track unless new evidence shows the settled architecture is insufficient.

## 3. Current Codebase Pressure Points

The following observations are based on the current post-`#381` codebase.

### 3.1 Backend/runtime composition is still denser than ideal

Relevant files:

- `internal/api/tui_backend.go`
- `internal/engine/engine.go`

Current shape:

- `api.AppBackend` is the right in-process application-facing owner, but it still directly aggregates several concerns:
  - `Store`
  - `Client`
  - `Syncer`
  - `Enricher`
  - user identity binding
  - event-publication callbacks
- `api.NewAppBackend(...)` has a long constructor signature and mixes:
  - dependency wiring
  - identity resolution rules
  - event hook injection
- `engine.NewCoreEngine(...)` is the right top-level owner, but it still performs a large amount of inline composition:
  - shared-service construction
  - event closure wiring
  - backend construction
  - runtime host assembly

Why this matters:

- this is not an ownership problem anymore
- it is a local readability and extension-safety problem
- richer native features will likely add new orchestration and composition pressure here first

### 3.2 The TUI remains the largest stateful internal surface

Relevant files:

- `internal/tui/model.go`
- `internal/tui/update.go`
- `internal/tui/actions.go`

Current shape:

- `tui.Model` still carries a large amount of state and collaborator wiring
- `NewModel(...)` validates and initializes many dependencies and state concerns at once
- `Update(...)` is cleaner than before, but it still combines:
  - bridge-status refresh
  - transition interpretation
  - action execution
  - sub-model update delegation
  - follow-up scheduling
- action methods are improved after backend cleanup, but still combine optimistic local UI behavior with task submission at the TUI edge

Why this matters:

- the TUI is now architecturally cleaner, but still the easiest place for local complexity to accumulate again
- future native work is easier if the TUI remains a predictable canonical client

### 3.3 Traffic control is readable, but still a careful subsystem

Relevant file:

- `internal/api/traffic.go`

Current shape:

- the recent refactor improved `runTask(...)` readability by extracting intent-named helpers
- the controller still relies on:
  - a fixed polling supervisor
  - explicit worker-limit bookkeeping
  - queue-level policy decisions
  - coordinated shutdown behavior

Why this matters:

- this code is no longer structurally alarming
- it is still one of the most operationally dense areas in the repo
- future feature pressure will make review cost rise quickly if local policy remains hard to scan

### 3.4 Event semantics are better internally than externally

Relevant files:

- `internal/engine/events.go`
- `internal/engine/mcp.go`

Current shape:

- internal event names now distinguish:
  - notification list changes
  - notification enrichment changes
- MCP translation still intentionally maps both to the same external `resources/list_changed` notification

Why this matters:

- this is acceptable today
- the risk is not current breakage
- the risk is future client pressure making coarse external observation harder to reason about

### 3.5 Transport seams are cleaner, but still specialized

Relevant files:

- `internal/engine/mcp.go`
- `internal/engine/adapter.go`

Current shape:

- backend-delegated mutation paths are cleaner
- result shaping is tighter
- remaining transport-specific exceptions are explicit
- the adapter still owns specialized decode/debounce/reload behavior

Why this matters:

- transport is no longer the top internal problem
- but it still needs discipline so transport-specific logic does not slowly reclaim behavior ownership

## 4. Refinement Principles

The following principles should govern this track:

- prefer changes that reduce local surprise over changes that merely improve naming aesthetics
- keep user-visible behavior stable unless a specific bug or surprise reduction requires otherwise
- favor clearer construction, composition, and local policy expression over broader abstraction
- make the dense seams easier to reason about before adding richer native-facing workflows
- keep the architecture-v2 runtime model intact unless later evidence shows it is insufficient

## 5. Recommended Starting Slice

Start with:

- **reduce local surprise and composition density around backend/runtime composition**

Primary targets:

- `internal/api/tui_backend.go`
- `internal/engine/engine.go`

The goal of this first slice should be to make the current backend/runtime assembly easier to read and extend without changing the settled architecture.

Examples of the kind of changes this slice may include:

- make `AppBackend` construction less argument-dense
- separate identity binding and event hook injection from the core dependency bundle more clearly
- make `NewCoreEngine(...)` composition stages easier to scan
- reduce the amount of inline wiring that a reader must mentally reconstruct to understand the runtime

Why this should come first:

- it offers the highest leverage for future extension work
- it reduces surprise in the seam where more orchestration pressure is likely to land
- it is more substantive than another naming-only cleanup, while still avoiding broad architecture churn

## 6. Recommended Order After The Starting Slice

The recommended order is:

1. reduce backend/runtime composition density
2. simplify TUI local flow and state-transition handling
3. deepen traffic-controller policy readability
4. tighten event-observation semantics where consumer pressure justifies it
5. add targeted tests and observability around dense seams

Rationale:

- composition clarity should improve first, because it is the highest-leverage extension point
- the TUI is the next largest local-complexity surface
- traffic control is dense but operationally stable, so it should be improved after the more central composition and UI seams
- event and observability work should be driven by real client pressure
- targeted tests should reinforce the above changes instead of becoming a detached cleanup effort

## 7. What To Defer

Defer these until later evidence makes them necessary:

- another architecture-level redesign
- transport migration or transport-primary redesign work
- broad package relocation for structural symmetry alone
- concurrency model replacement in the traffic controller
- generalized abstraction layers that exceed the needs of the current project scale

## 8. Follow-up Issue Breakdown

The following issue breakdown is intended to be concrete enough for direct issue creation.

### Issue 1: Reduce backend/runtime composition density

Recommended starting point: start here first.

- Scope:
  - clarify `AppBackend` construction and dependency bundling
  - make `NewCoreEngine(...)` composition stages easier to understand at a glance
  - reduce local surprise in the current runtime assembly path
- Preserved invariant:
  - no architecture-v2 ownership or authority changes
  - no user-visible behavior change
- Validation:
  - targeted `internal/api` and `internal/engine` tests
  - `make go/check`

### Issue 2: Simplify TUI local flow and state transitions further

- Scope:
  - reduce local coordination density in `Model`, `Update(...)`, and adjacent action/result flow
  - keep backend-owned behavior while making TUI-local state flow easier to scan
- Preserved invariant:
  - current TUI-visible behavior remains stable
  - backend continues to own mutation semantics
- Validation:
  - targeted `internal/tui` tests
  - `make go/check`

### Issue 3: Deepen traffic-controller policy readability

- Scope:
  - continue separating policy expression from coordination mechanics
  - improve scanability of lockout, quota, and worker/queue decisions without redesigning the supervisor model
- Preserved invariant:
  - current queueing, lockout, and shutdown behavior remain stable
- Validation:
  - targeted traffic-controller tests
  - `make go/check`

### Issue 4: Tighten event observation and translation discipline

- Scope:
  - improve event semantics or surrounding documentation/tests where coarse translation creates real consumer ambiguity
  - keep internal vs external event intent explicit
- Preserved invariant:
  - existing observers continue to receive required updates
  - transport does not reclaim behavior ownership
- Validation:
  - targeted engine/backend tests
  - `make go/check`

### Issue 5: Add targeted surprise-reduction tests and observability checks

- Scope:
  - add narrowly chosen tests or observability assertions around the densest seams
  - focus on the places most likely to create unexpected behavior during future extension work
- Preserved invariant:
  - no broad test-suite redesign
  - no speculative instrumentation expansion
- Validation:
  - targeted tests for touched packages
  - `make go/check`

## 9. Success Criteria

This refinement track is succeeding if:

- future follow-up issues are smaller and easier to reason about than the pre-v2 architecture work
- dense seams become easier to scan without broad redesign
- unexpected behavior becomes less likely because local policy and composition are easier to see directly in code
- the project stays extensible for richer native features without reopening the core architecture decisions
