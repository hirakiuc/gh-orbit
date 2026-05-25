# Transport Decision Record

This document records the approved post-cleanup transport evaluation from issue `#364`.

It compares:

- MCP-only
- MCP plus gRPC
- gRPC-primary

This is a design decision record, not an implementation plan.

## 1. Decision statement

Current decision:

- keep MCP as the current app-facing transport
- do not replace MCP immediately
- defer any gRPC implementation until there is clear pressure that the cleaned backend seam cannot satisfy efficiently through MCP alone
- if transport pressure grows, prefer evaluating `MCP + gRPC` before a full `gRPC-primary` replacement

This is a provisional default based on the current architecture and product scope. It is not a permanent rule and may be revisited if the conditions in section 8 are met.

## 2. Architectural baseline

This decision is based on the cleaned architecture after issues `#357` through `#363`.

Relevant properties of that baseline:

- one backend authority is explicit
- the TUI uses a transport-agnostic backend seam
- the native app remains a host around the same backend runtime
- embedded TUI support remains a hard requirement
- MCP has been narrowed toward an adapter role rather than a behavior-owning layer
- runtime ownership and config authority are now explicit enough to evaluate transport separately from backend design

This means the transport question can now be evaluated on cleaner boundaries than before.

## 3. Evaluation criteria

The transport options were compared against these criteria:

- fit with the cleaned backend seam
- support for standalone TUI, embedded TUI, and native app hosting
- event and streaming semantics
- typed client ergonomics for Go and Swift
- implementation complexity and migration cost
- compatibility with tool- or agent-oriented integration
- operational simplicity for a local-first single-user product

## 4. Option comparison

### 4.1 MCP-only

Strengths:

- already implemented and integrated
- fits the current local engine and native app interaction model
- preserves existing tool- and agent-oriented compatibility
- no transport rewrite cost
- sufficient for current single-user scope

Weaknesses:

- weaker typed client ergonomics than gRPC, especially for Swift
- protocol concepts are less natural for app-style RPC than for tool-style interaction
- streaming and event handling are workable, but less conventional than gRPC for rich native clients

Assessment:

- strong near-term fit
- acceptable long-term fit if app-facing transport demands remain moderate

### 4.2 MCP plus gRPC

Strengths:

- keeps MCP for tool/agent interoperability
- adds a more conventional typed app-facing transport for native clients
- allows gradual adoption without replacing the current integration path immediately
- best matches the “transport as adapter” architecture if both client styles matter

Weaknesses:

- highest transport-surface complexity
- duplicate adapter maintenance unless boundaries remain disciplined
- introduces operational and testing overhead for two protocols

Assessment:

- strongest long-term flexibility
- not justified yet unless native typed-client pressure becomes materially stronger

### 4.3 gRPC-primary

Strengths:

- strong typed contract generation
- conventional streaming model
- clearer app-style client ergonomics for Swift and Go

Weaknesses:

- highest near-term migration cost
- would either drop MCP compatibility or force MCP into a secondary path anyway
- adds substantial change pressure without current evidence that MCP is blocking product progress

Assessment:

- technically attractive for a future app-platform shape
- not justified as the current default for this project

## 5. Decision rationale

The current architecture no longer has to use MCP as its internal design shape. That was the necessary precondition for making a transport decision responsibly.

With that cleanup complete, the remaining question is product pressure:

- today, MCP is good enough for the current local-first, single-user system
- the native app requirement is real, but it does not yet by itself justify a transport migration
- a full gRPC move now would add substantial cost before there is evidence that MCP is the bottleneck

That leads to the current decision:

- keep MCP now
- keep the backend seam transport-agnostic
- revisit gRPC only if native app or multi-client needs become strong enough to outweigh the added transport complexity

## 6. What this decision does not mean

This decision does not mean:

- MCP is the permanent long-term answer
- gRPC is rejected in principle
- the project should avoid typed app-facing transport forever
- transport work is more important than backend clarity

It means only that, on the cleaned architecture and current scope, immediate replacement is not the best next move.

## 7. Recommended sequencing

If transport work becomes necessary later, the recommended order is:

1. keep the backend seam stable
2. define the app-facing RPC surface from backend use cases
3. evaluate whether gRPC should be added alongside MCP first
4. only consider gRPC-primary replacement if the hybrid model proves unnecessarily costly

This sequence preserves the architecture-v2 rule that transport remains secondary to backend design.

## 8. Revisit conditions

Re-open this decision if one or more of these become true:

- Swift/native development is materially slowed by MCP’s weaker typed-client ergonomics
- event or streaming requirements become awkward enough that MCP adaptation adds repeated complexity
- multiple richer local clients appear and app-style RPC becomes more important than tool-style interoperability
- MCP compatibility stops being valuable relative to the maintenance cost
- transport adapter code starts absorbing behavior pressure again despite the cleaned backend seam

If those conditions appear, the first alternative to evaluate should be `MCP + gRPC`, not an immediate all-at-once replacement.

## 9. Final recommendation

Recommendation for the current project state:

- continue with MCP as the only implemented transport for now
- treat gRPC as a possible later enhancement, not an immediate migration target
- prefer a future hybrid `MCP + gRPC` path over a direct `MCP -> gRPC-primary` rewrite if stronger transport pressure emerges
