# Strategy Review Workflow (Agentic & Role-Based)
*Mandatory planning phase for all non-trivial changes to ensure architectural alignment and local speed.*

## The Static Workbench
To minimize coordination overhead and maximize token efficiency, this workflow uses three static file paths:
- **Active Proposal**: `.agents/proposal.md` (Live design workbench).
- **Active Context**: `.agents/issue.md` (Cache for target GitHub Issue description).
- **Active Feedback**: `.agents/feedback.md` (Live audit log from the Reviewer).

---

## Roles

### 1. Worker (Author)
- **Responsibility**: Researches the codebase and drafts the implementation plan.
- **Context Management**: MUST fetch the target GitHub Issue description and save it to `.agents/issue.md` before starting.
- **Goal**: Propose a surgical solution that adheres to `AGENTS.md` and `GEMINI.md`.
- **Knowledge Acquisition**: Actively research required knowledge online. If information remains insufficient, **Ask the User**.
- **Primary Tool**: `.agents/proposal.md` (Overwritten per task).

### 2. Reviewer (Auditor)
- **Responsibility**: Critiques the proposal for security, testability, and architecture.
- **Workflow**: MUST execute the `.agents/workflows/feedback.md` workflow.
- **Context Discovery**: MUST look for the "Reviewer Hint" in `.agents/proposal.md` and read the referenced context in `.agents/issue.md` before auditing.
- **Output**: Results MUST be persisted to `.agents/feedback.md`.

---

## Procedure (The Hybrid Loop)

### Phase A: Local Iteration (The Workbench)
1. **[Worker] Selection**: Pick a target Issue from **Project #7** or `make roadmap`.
2. **[Worker] Initialization**: Run `make task ID="<issue-id>"` to automate the workbench setup.
   - *Note*: This resets **Revision** to 1 in `.agents/proposal.md`.
3. **[User] Trigger**: The user instructs the Reviewer to start the review.
4. **[Reviewer] Audit**: Read `.agents/issue.md` and `.agents/proposal.md`, then execute the `feedback` workflow.
5. **[Reviewer] Persist**: Save findings to `.agents/feedback.md` and notify the Worker.
6. **[Worker] Refine**: Update `.agents/proposal.md` and **increment the Revision number** based on feedback.
7. **[Reviewer/User] Sign-off**: Once satisfied, the Reviewer provides the **SIGN-OFF** marker. 

   - *Escape Hatch*: The User can provide direct approval if a stalemate occurs.

### Phase B: GitHub Synchronization (The Record)
8. **[Worker] Publish**: Post the *final, signed-off* content of `.agents/proposal.md` as a comment on the GitHub Issue.
   - `gh issue comment "<ID>" --body-file .agents/proposal.md`
9. **[Worker] Branching**: Create the development branch linked to the issue.
   - `gh issue develop "<ID>"`
10. **[Worker] Implementation**: Execute the plan on the new branch.


## Rationale
- **Zero-Config Coordination**: Agents always know exactly which files to read/write without human intervention.
- **Clean Workspace**: Only the active task's design state exists locally.
- **Permanent Archive**: The GitHub Issue provides the historical design context, allowing local cleanup.
- **Robustness**: Online research and User-led triggers ensure agents don't hallucinate or loop infinitely.
