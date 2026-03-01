# Strategy Review Workflow
*Mandatory planning phase for all non-trivial changes.*

## Procedure
1. **Research**: Map codebase, identify dependencies and side effects.
2. **Propose**: Create `.agent/proposals/<name>.md` using [TEMPLATE.md](./TEMPLATE.md).
3. **Review**: Present to user/reviewer. **Wait for SIGN-OFF** marker before modifying `src/`.
4. **Execute**: Surgical implementation. Verify via Proposal plan.
5. **Cleanup**: Delete proposal after merge.

## Rationale
Prevent Prevent uncoordinated changes, ensure alignment with project goals, and maintain code integrity.
