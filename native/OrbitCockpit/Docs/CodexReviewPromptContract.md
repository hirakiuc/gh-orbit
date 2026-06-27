# Codex Review Prompt Contract

Version: `gh-orbit-review-contract/v1`

This contract defines the initial prompt sent to Codex CLI when Orbit Cockpit launches a managed review workspace session.

Required context:

- repository identity
- pull request number and URL
- head repository, branch, and SHA
- prepared review worktree path
- repository instructions loaded from `AGENTS.md` when available
- review-only objective and expected terminal-only output

Constraints:

- launch Codex via direct executable and argument invocation
- pass the prepared review worktree as the terminal launch working directory
- do not interpolate PR metadata into a shell command
- do not include clone URLs, credentials, tokens, or other secrets in the prompt
- do not instruct Codex to push, merge, comment on GitHub, or persist structured review output in this phase

Expected behavior:

- Codex prints findings and completion status directly to the attached terminal session
- Orbit Cockpit supervises only the terminal process lifecycle and does not parse or store structured review results
