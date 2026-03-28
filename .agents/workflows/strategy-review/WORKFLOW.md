---
description: Mandatory strategy review workflow for all non-trivial changes in the gh-orbit project.
---
# Strategy Review Workflow (Agentic & Role-Based)

*Mandatory planning phase for all non-trivial changes to ensure architectural alignment and local speed.*

## The Static Workbench

To minimize coordination overhead and maximize token efficiency, this workflow uses three static file paths:

- **Active Proposal**: `.agents/proposal.md` (Live design workbench).
- **Active Context**: `.agents/issue.md` (Cache for target GitHub Issue description).
- **Active Feedback**: `.agents/feedback.md` (Live audit log from the Reviewer).
- **Optional RFC**: `.agents/rfc.md` (Persistent log for high-level architectural discussion).

---

## Roles

### 1. Worker (Author)

- **Responsibility**: Researches the codebase and drafts the implementation plan.
- **Context Management**: MUST fetch the target GitHub Issue description and save it to `.agents/issue.md` before starting.
- **Goal**: Propose a surgical solution that adheres to `AGENTS.md` and `GEMINI.md`.
- **Knowledge Acquisition**: Actively research required knowledge online. If information remains insufficient, **Ask the User**.
- **Primary Tool**: `.agents/proposal.md` (Overwritten per task).
- **Proof of Correctness**: MUST define a reproduction plan or test contract in Section 4 of the proposal.

### 2. Reviewer (Auditor)

- **Responsibility**: Critiques the proposal for security, testability, and architecture.
- **Workflow**: MUST execute the `.agents/workflows/feedback.md` workflow.
- **Context Discovery**: MUST look for the "Reviewer Hint" in `.agents/proposal.md` and read the referenced context in `.agents/issue.md` before auditing.
- **Output**: Results MUST be persisted to `.agents/feedback.md`.

---

## Procedure (The Hybrid Loop)

### Phase A: Selection & Choice of Path

1. **[Worker] Selection**: Pick a target Issue from **Project #7** or `make roadmap`.
2. **[Worker] Initialization**: Run `make task ID="<issue-id>"` to automate the workbench setup.
   - *Note*: This **resets Revision to 1** in `.agents/proposal.md`.
3. **[Worker] Decision Gate**: Evaluate the task's complexity to choose the appropriate path:

#### Path 1: The RFC Path (High Uncertainty / Systemic Change)
**Use when**:
- Architectural shifts or new external dependencies.
- High Blast Radius (impacting 3+ packages or core interfaces).
- Open design questions with multiple valid solutions.

1. **[Worker] Draft RFC**: Share a high-level strategy summary. For complex cases, use `.agents/rfc.md` to maintain persistence.
   - *Security Note*: Never include sensitive credentials or PII in RFC files or discussion logs.
2. **[User/Reviewer] Alignment**: Discuss the RFC to reach high-level consensus.
3. **[Worker] Transition**: Once aligned, move to **Path 2** to formalize the implementation.

#### Path 2: The Proposal Path (Refined Implementation)
**Use when**:
- Features, bug fixes, or well-defined patterns.
- Following a successful RFC alignment.

1. **[Worker] Formalize**: Draft the formal `.agents/proposal.md` using the template.
   - *Mandatory*: Explicitly address trade-offs in **Section 1.1 "Discussion & Trade-offs"**.
2. **[User] Trigger**: The user instructs the Reviewer to start the review.
3. **[Reviewer] Audit**: Read `.agents/issue.md` and `.agents/proposal.md`, then execute the `feedback` workflow.
4. **[Reviewer] Persist**: Save findings to `.agents/feedback.md` and notify the Worker.
5. **[Worker] Refine**: Update `.agents/proposal.md` and **increment the Revision number** based on feedback.
6. **[Reviewer/User] Sign-off**: Once satisfied, the Reviewer provides the **SIGN-OFF** marker.

   - *Escape Hatch*: The User can provide direct approval if a stalemate occurs.

### Phase B: Implementation & Submission

1. **[Worker] Publish**: ONLY after receiving a **SIGN-OFF**, synthesize the final technical context of `.agents/proposal.md` into a clean architectural record (the **Approved Strategy**) using the `FINAL_RECORD_TEMPLATE.md`.
   - Post this record as a comment on the GitHub Issue: `gh issue comment "<ID>" --body "<SYNTHESIZED_RECORD>"`
   - *Note*: Ensure all procedural noise (Revision, Reviewer Hints, Review History) is omitted to maintain a professional project history.
2. **[Worker] Branching**: Create the development branch linked to the issue.
   - `gh issue develop "<ID>"`
3. **[Worker] Implementation**: Execute the plan on the new branch, ensuring adherence to the Approved Strategy.
4. **[Worker] Submission**: Create a Pull Request using `gh pr create`.
   - *Requirement*: The PR description MUST reference the Issue ID and summarize the final implementation.

### Phase C: Implementation Audit (The Closing Loop)

1. **[Reviewer/User] Audit**: Perform the final code review on the Pull Request.
   - **Verification**: The Reviewer MUST explicitly verify the final diff on the topic branch against the **Approved Strategy** (published in Phase B) before providing approval.
2. **[Worker] Iteration**: Address feedback on the PR until the Reviewer is satisfied that the implementation matches the intent of the Approved Strategy.
3. **[Worker] Merge**: Merge the PR once the **SIGN-OFF** (Approval) is granted.
4. **[Worker] Synchronize**: Switch back to the default branch (`main`) and pull the latest changes.
5. **[Worker] Terminal Reset**: **Mandatory step.** Run `make reset-task` to clear the local workbench and prepare the filesystem for the next task.

## Rationale

- **Zero-Config Coordination**: Agents always know exactly which files to read/write without human intervention.
- **Clean Workspace**: Only the active task's design state exists locally.
- **Permanent Archive**: The GitHub Issue provides the historical design context, allowing local cleanup.
- **Robustness**: Online research and User-led triggers ensure agents don't hallucinate or loop infinitely.
- **Verification Integrity**: Mandatory "Proof of Correctness" ensures that testing is part of the architectural design.
- **Architectural Synergy**: The RFC phase ensures high-level alignment before committing to implementation details.
