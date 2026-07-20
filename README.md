# 🛰️ gh-orbit

**High-fidelity triage and multi-agent AI orchestration for GitHub.**

> [!IMPORTANT]
> This project is currently under active development. There is no official support provided at this time.

`gh-orbit` is a "Hybrid Host" triage system. It combines a high-performance **Headless Go Engine** with both a **Terminal UI (TUI)** and a native **macOS Cockpit** to provide a unified command center for managing GitHub notifications and AI-assisted code reviews.

---

## 🚀 Quick Start

### Installation (CLI)

Ensure you have the [GitHub CLI](https://cli.github.com/) installed, then run:

```bash
gh extension install hirakiuc/gh-orbit
```

### Usage (TUI)

Simply run:

```bash
gh orbit
```

*No API keys or Personal Access Tokens required. It inherits your existing `gh` credentials automatically.*

---

## 🏛️ Architecture

`gh-orbit` follows a decoupled architecture using the **Model Context Protocol (MCP)** over secure Unix Domain Sockets:

1. **Core Engine (Go)**: The authority for local SQLite state, GitHub API interactions, background services, and mutation events.
2. **Terminal UI**: The standard cross-platform triage interface. It uses the engine in-process in standalone mode or through MCP when a headless engine is available.
3. **Orbit Cockpit (macOS)**: A native SwiftUI host that starts or reuses the headless engine and embeds terminal panes for TUI navigation and AI agent execution logs.

---

## 🧭 Product Direction

`gh-orbit` is built for high-volume GitHub workflows where notification triage
must stay fast even as humans and AI agents produce more pull requests. Its
local-first model keeps notification and triage state in private SQLite storage,
supports offline review of synchronized data, and makes individual or batch
organization responsive without requiring manual token management.

The current product focuses on dependable notification triage and review
orchestration. Future AI-assisted capabilities may add concise pull-request
summaries, locally grounded risk signals, and failed-CI diagnostics. Those
features should remain optional, preserve backend authority, and clearly expose
the source data behind generated conclusions.

---

## ⌨️ Cheat Sheet (Shortcuts)

| Key | Action |
| :--- | :--- |
| `r` | Sync notifications (Manual) |
| `m` | Toggle Read/Unread state |
| `x` | Toggle local Handled/Unhandled state |
| `S` | Enter or leave multiple-selection mode |
| `s` | Add or remove the current notification from the selection |
| `R` / `U` | Mark selected notifications Read / Unread |
| `H` / `N` | Mark selected notifications Handled / Unhandled locally |
| `enter` | View notification details (Description/Body) |
| `o` | Open in default browser |
| `c` | Checkout PR locally (`gh pr checkout`) |
| `1`-`3` | Set local priority (Low/Med/High) |
| `0` | Clear local priority |
| `p` / `i` | Filter by Pull Request / Issue |
| `tab` | Cycle through tabs (Inbox, Unread, Triaged, All) |
| `?` | Toggle detailed help menu |
| `q` / `esc` | Back to list / Quit |

Read state and local handled (triaged) state are independent. The `m` binding
changes GitHub/local read state, while `x` changes only Inbox triage state.
`keys.toggle_handled` is configurable; existing bindings take precedence over
the inherited `x` default, and `[]` disables the action. If you explicitly add
`toggle_handled` to your config and later downgrade to a version from before
Issue #473, remove that one line first because older versions strictly reject
unknown configuration keys.

### Multiple selection and batch operations

Batch operations use a direct, Vim-style workflow with no confirmation dialog:

1. Press `S` to enter multiple-selection mode.
2. Navigate normally and press `s` to add or remove the current notification.
3. Press `R`, `U`, `H`, or `N` to apply that state to every selected item.

For example, `S`, `s`, `j`, `j`, `s`, `R` selects the current notification and
another notification two rows below it, then marks both as read. The `▌`
indicator remains the list cursor, while `✓` marks every notification included
in the batch. The footer reports the selected count and changes from `SELECT`
to `APPLYING`, `REFRESH`, or `UNCERTAIN` as the operation progresses.

`R` updates local read state and sends bounded per-notification read requests to
GitHub. `U` is local-only because GitHub does not provide an arbitrary
mark-thread-unread operation. `H` and `N` change only gh-orbit's local handled
state. If some remote read requests fail, the unsuccessful notifications remain
selected for retry. A single batch accepts at most 100 distinct notifications.

Press `esc` or `S` again to cancel and clear the selection. Changing tabs or
filters, or entering the detail view, also clears it. All batch bindings are
configurable through `keys.selection_mode`, `keys.select_notification`,
`keys.batch_read`, `keys.batch_unread`, `keys.batch_handled`, and
`keys.batch_unhandled` in the gh-orbit configuration file.

---

## 🛠️ Development

### Prerequisites

- **Go 1.22+**: Required for the core engine.
- **Xcode 16.0+ / Swift 6.0+**: Required for the native macOS Cockpit.
- **rumdl 0.2.36+**: Required for Markdown linting (`brew install rumdl`).
- **Nerd Fonts**: Required for icons.

### Building

The project uses a namespaced `Makefile` for multi-language orchestration:

```bash
make build          # Build the Go core binary
make cockpit        # Build the native macOS .app bundle
make check          # Run all quality gates (Go + Native)
```

Run `make help` for a complete list of `go/` and `native/` specific targets.

---

## 🔒 Security & Privacy

- **Local-First**: Triage state is stored in a private local SQLite database (`modernc.org/sqlite`).
- **MCP Security**: Unix Domain Sockets use mandatory **Peer Verification** (PID + Code Signature).
- **Sandbox Note**: The macOS Cockpit has sandboxing disabled to allow for native PTY management and subprocess control.
- **Auth**: Inherits credentials from the `gh` host environment.

---

## 🩺 Health Check

If you encounter issues, run the diagnostic suite:

```bash
gh orbit doctor
```

---

## 📄 License

License information has not yet been published.

*For technical depth, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [AGENTS.md](AGENTS.md).*
