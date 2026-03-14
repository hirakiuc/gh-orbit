---
name: gh-extension-expert
description: Specialized guidance for building GitHub CLI extensions using Go, specifically for the gh-orbit project. Includes rules for naming, authentication, and distribution.
---

# GitHub CLI Extension Expert: gh-orbit

This skill provides mandatory guidelines for developing `gh-orbit` as a valid GitHub CLI extension.

## Extension Identity

- **Repository Name**: `gh-orbit` (Required prefix `gh-`).
- **Command Name**: `gh orbit` (Determined by the part after the prefix).
- **Topic**: Ensure the repository has the `gh-extension` topic for discoverability.

## Implementation Guidelines (Go)

- **Authentication**: Use the `go-gh` library to inherit the user's `gh` token.
  - Reference: `github.com/cli/go-gh/v2`
- **Machine Readability**: When calling core `gh` commands, always use `--json` to ensure robust parsing.

## Distribution & Naming

For Go extensions, binaries must follow a strict naming convention to be correctly handled by `gh extension install`.

### Binary Naming Pattern

`gh-orbit-<OS>-<ARCH>[.exe]`

| OS | ARCH | Asset Name |
| :--- | :--- | :--- |
| macOS | arm64 | `gh-orbit-darwin-arm64` |
| macOS | amd64 | `gh-orbit-darwin-amd64` |
| Linux | amd64 | `gh-orbit-linux-amd64` |
| Windows | amd64 | `gh-orbit-windows-amd64.exe` |

### Automated Releases

Use the `gh-extension-precompile` GitHub Action to automate cross-compilation and asset naming.

## Local Development

To test the extension locally:

```bash
gh extension install .
```

For more detailed patterns and checklists, see the [references](references/guidelines.md).
