# Post-V2 Internal Refactoring Plan

This document records the approved planning direction from issue `#374`.

It defines the next internal improvement track after `ARCHITECTURE_V2` and its first implementation roadmap are complete.

This is a planning document, not an implementation change.

## 1. Planning objective

The goal of this plan is to improve extensibility and maintainability before the project takes on more complex native-app features.

The architecture-v2 work removed the largest ownership and boundary ambiguities. That changes the next problem:

- the main risk is no longer large-scale architecture confusion
- the main risk is internal sharpness under future growth

This plan is meant to keep the next round of work incremental, practical, and high leverage.

## 2. Current baseline

After issues `#357` through `#364`, the project now has:

- one cleaner backend authority
- a narrowed TUI-facing backend seam
- transport treated more as adapter than behavior owner
- explicit runtime ownership and config authority
- a documented transport decision that keeps MCP now and defers any gRPC move

That means the next improvement opportunities are mostly internal:

- service and package shape
- event semantics
- controller readability
- TUI state-management clarity
- translation-boundary sharpness
- test layering

## 3. Improvement principles

The following principles should govern this track:

- prefer incremental refactoring over broad redesign
- optimize for future native-feature pressure, not abstract purity
- keep user-visible behavior stable while internals improve
- avoid adding framework-like indirection that exceeds the needs of a single-user project
- keep transport secondary to backend architecture

## 4. Prioritized tracks

### Track 1: Service and package shape cleanup

Focus:

- clarify the current backend/application layer shape
- reduce mixed responsibilities inside the present service package structure
- make ownership and orchestration easier to understand from type names and constructors

Why this matters:

- this is the highest-leverage readability and extensibility improvement left after the v2 boundary cleanup
- more native features will put pressure on orchestration paths first

Risks:

- package churn without enough behavioral payoff

Guidance:

- prefer small structural clarifications over large package moves
- only rename or split when it reduces real confusion

### Track 2: Event semantics tightening

Focus:

- make EventBus signaling more precise
- distinguish notification refresh, enrichment update, and session/transport state more clearly
- reduce places where consumers infer too much from coarse signals

Why this matters:

- richer native features will depend more on precise state observation
- clearer event semantics reduce duplicated refresh logic and accidental coupling

Risks:

- over-designing an event taxonomy too early

Guidance:

- improve precision only where a real consumer benefit is visible

### Track 3: Traffic-controller internal refactoring

Focus:

- improve readability and extensibility of the traffic controller
- separate policy decisions more clearly from queue/execution mechanics
- document or simplify the current coordination model further where useful

Why this matters:

- this is still one of the most mechanically dense internal areas
- future feature growth increases the cost of unclear coordination code

Risks:

- concurrency rewrites can create subtle regressions without strong payoff

Guidance:

- favor targeted extraction and policy clarification over redesign

### Track 4: TUI internal simplification

Focus:

- reduce remaining policy residue in TUI action/update paths
- keep state transitions and command issuance easier to reason about
- improve internal consistency of TUI message/result flow

Why this matters:

- the TUI is healthier than before, but still a large stateful surface
- more complex native work will be easier if the TUI remains a clean canonical client

Risks:

- unnecessary churn in stable user-facing interaction code

Guidance:

- prioritize clarity wins that reduce future maintenance cost directly

### Track 5: Transport translation cleanup

Focus:

- tighten remaining duplication or awkwardness between adapter and server translation layers
- keep temporary exceptions explicit
- prevent transport concerns from slowly reclaiming behavior ownership

Why this matters:

- transport is now in a better place architecturally, so preserving that boundary matters

Risks:

- spending too much effort on transport internals before native or product needs require it

Guidance:

- keep this behind the higher-leverage internal tracks above

### Track 6: Test layering and contract coverage

Focus:

- improve separation between behavior, orchestration, transport, and UI tests
- add clearer contract tests around backend seam behavior and event delivery
- reduce tests that incidentally assert too many layers at once

Why this matters:

- internal refactoring gets cheaper when the test layers are more intentional

Risks:

- large test churn without enough corresponding design benefit

Guidance:

- use this track to support the refactors above, not as a standalone test expansion exercise

## 5. Recommended order

The recommended order is:

1. service and package shape cleanup
2. event semantics tightening
3. traffic-controller internal refactoring
4. TUI internal simplification
5. transport translation cleanup
6. test layering and contract coverage follow-through

Rationale:

- Track 1 improves the mental model of the current backend most directly
- Track 2 improves observability and prepares for richer native-facing state needs
- Track 3 addresses the densest remaining coordination code
- Track 4 benefits from the cleaner service/event foundation
- Track 5 should happen only after the higher-value internal seams are clearer
- Track 6 should reinforce the refactors above rather than run ahead of them

## 6. Recommended starting slice

Start with:

- service and package shape cleanup around the current backend/application layer

This is the recommended first slice because it offers the highest leverage with the least strategic risk. It improves readability, future extension paths, and later refactor safety without reopening the already-settled architecture-v2 decisions.

## 7. What to defer

Defer these until the above track work shows a stronger need:

- transport migration work
- another architecture-level redesign
- broad package relocation for cosmetic alignment only
- concurrency model rewrites without operational evidence
- native feature work that depends on internals becoming cleaner first

## 8. First-wave issue breakdown

### Issue 1: Clarify backend/application service shape

Recommended starting point: start here first.

- Scope:
  - identify the most confusing remaining service/package boundaries around the current backend/application layer
  - make ownership and orchestration easier to read from constructors and type roles
- Preserved invariant:
  - no user-visible behavior change
  - no reopening of architecture-v2 ownership decisions
- Validation:
  - targeted Go tests for touched packages
  - `make go/check`

### Issue 2: Tighten EventBus and event semantics

- Scope:
  - make event intent more explicit where coarse signaling still creates ambiguity
  - improve distinctions among notification refresh, enrichment updates, and session state
- Preserved invariant:
  - existing consumers keep receiving required updates
  - event precision increases without introducing transport-led behavior
- Validation:
  - targeted engine/backend tests
  - `make go/check`

### Issue 3: Refactor traffic-controller internals incrementally

- Scope:
  - simplify policy/mechanics separation
  - improve readability without replacing the current coordination model wholesale
- Preserved invariant:
  - current queueing, lockout, and rate-limit behavior remains stable
- Validation:
  - targeted traffic-controller tests
  - `make go/check`

### Issue 4: Simplify TUI internal state and command flow

- Scope:
  - reduce remaining policy residue in TUI internals
  - improve consistency of state transitions and backend-result handling
- Preserved invariant:
  - current TUI-visible behavior remains stable
- Validation:
  - targeted TUI tests
  - `make go/check`

### Issue 5: Tighten transport adapter/server translation boundaries

- Scope:
  - reduce remaining duplication and keep exceptions explicit
- Preserved invariant:
  - transport remains adapter-shaped and does not retake behavior ownership
- Validation:
  - targeted MCP/adapter tests
  - `make go/check`

### Issue 6: Improve test layering and contract coverage

- Scope:
  - reinforce the earlier refactors with clearer behavior/orchestration/transport contract coverage
- Preserved invariant:
  - test improvements support refactoring rather than drive unnecessary churn
- Validation:
  - targeted Go tests
  - `make go/check`

## 9. Final recommendation

The next internal quality track should begin with service/package shape clarity, then tighten event semantics, then address the traffic controller, and only afterward spend meaningful effort on lower-leverage translation cleanup or broader test reshaping.

That sequence is the best fit for the project’s current state:

- the big architecture work is done
- native-feature pressure is likely to rise next
- the highest value now comes from making the current internals easier to extend safely
