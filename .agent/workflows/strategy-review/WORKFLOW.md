# Strategy Review Workflow (Agentic & Role-Based)
*Mandatory planning phase for all non-trivial changes to ensure architectural alignment and local speed.*

## The Static Workbench
To minimize coordination overhead between agents, this workflow uses two static file paths:
- **Active Proposal**: `.agent/proposal.md`
- **Active Feedback**: `.agent/feedback.md`

---

## Roles

### 1. Worker (Author)
- **Responsibility**: Researches the codebase and drafts the implementation plan.
- **Goal**: Propose a surgical solution that adheres to `AGENTS.md` and `GEMINI.md`.
- **Primary Tool**: `.agent/proposal.md` (Overwritten per task).

### 2. Reviewer (Auditor)
- **Responsibility**: Critiques the proposal for security, testability, and architecture.
- **Workflow**: MUST execute the `.agent/workflows/feedback.md` workflow.
- **Output**: Results MUST be persisted to `.agent/feedback.md`.

---

## Procedure (The Hybrid Loop)

### Phase A: Local Iteration (The Workbench)
1. **[Worker] Selection**: Pick a target Issue from **Project #7** or `make roadmap`.
2. **[Worker] Draft**: Initialize or overwrite `.agent/proposal.md` using `TEMPLATE.md`. 
   - *Self-Correction*: Ensure the correct GitHub Issue ID is included in the header.
3. **[Worker] Request Review**: Signal the Reviewer to audit `.agent/proposal.md`.
4. **[Reviewer] Audit**: Execute the `feedback` workflow against `.agent/proposal.md`.
5. **[Reviewer] Persist**: Save findings to `.agent/feedback.md` and notify the Worker.
6. **[Worker] Refine**: Update `.agent/proposal.md` based on `.agent/feedback.md`.
7. **[Reviewer] Sign-off**: Once satisfied, provide the exact marker in the proposal: **SIGN-OFF**.

### Phase B: GitHub Synchronization (The Record)
8. **[Worker] Publish**: Post the *final, signed-off* content of `.agent/proposal.md` as a comment on the GitHub Issue.
   - `gh issue comment <ID> --body-file .agent/proposal.md`
9. **[Worker] Branching**: Create the development branch linked to the issue.
   - `gh issue develop <ID>`
10. **[Worker] Implementation**: Execute the plan on the new branch.

## Rationale
- **Zero-Config Coordination**: Agents always know exactly which files to read/write without human intervention.
- **Clean Workspace**: Only the active task's design state exists locally.
- **Permanent Archive**: The GitHub Issue provides the historical design context, allowing local cleanup.
