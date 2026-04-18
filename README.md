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

1. **Headless Engine (Go)**: The sole owner of the local SQLite database and GitHub API interactions. It broadcasts real-time mutation events via an internal event bus.
2. **Terminal UI (MCP Client)**: The standard cross-platform triage interface.
3. **Orbit Cockpit (macOS)**: A native SwiftUI application that hosts multiple terminal panes for TUI navigation and real-time AI agent execution logs.

---

## ⌨️ Cheat Sheet (Shortcuts)

| Key | Action |
| :--- | :--- |
| `r` | Sync notifications (Manual) |
| `m` | Toggle Read/Unread state |
| `enter` | View notification details (Description/Body) |
| `o` | Open in default browser |
| `c` | Checkout PR locally (`gh pr checkout`) |
| `1`-`3` | Set local priority (Low/Med/High) |
| `0` | Clear local priority |
| `p` / `i` | Filter by Pull Request / Issue |
| `tab` | Cycle through tabs (Inbox, Unread, Triaged, All) |
| `?` | Toggle detailed help menu |
| `q` / `esc` | Back to list / Quit |

---

## 🛠️ Development

### Prerequisites

- **Go 1.22+**: Required for the core engine.
- **Xcode 16.0+ / Swift 6.0+**: Required for the native macOS Cockpit.
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

MIT License. See [LICENSE](LICENSE) for details.

*For technical depth, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [AGENTS.md](AGENTS.md).*
