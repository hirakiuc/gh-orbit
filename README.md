# 🛰️ gh-orbit

**Manage your GitHub notifications without leaving the terminal.**

`gh-orbit` is a GitHub CLI extension that provides a high-performance TUI for triaging notifications. It's designed for speed, security, and "Zero-Config" ease of use.

---

## 🚀 Quick Start

### Installation

Ensure you have the [GitHub CLI](https://cli.github.com/) installed, then run:

```bash
gh extension install hirakiuc/gh-orbit
```

### Usage

Simply run:

```bash
gh orbit
```

*No API keys or Personal Access Tokens required. It inherits your existing `gh` credentials automatically.*

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

## 🛠️ Requirements

To ensure the TUI renders correctly, your terminal should support:

- **[Nerd Fonts](https://www.nerdfonts.com/font-downloads)**: Required for status and resource icons.
- **TrueColor**: Required for high-fidelity styling.

---

## 🔒 Security & Privacy

- **Local-First**: Your triage state (priorities, mutes) is stored in a private local SQLite database.
- **Strict Permissions**: All local data and logs are secured with `0700/0600` permissions.
- **Auth Scopes**: Uses your existing `gh` token with `notifications` and `repo` scopes.
- **Telemetry**: Optional OpenTelemetry traces are stored locally and encrypted via file permissions.

---

## 🩺 Health Check

If you encounter issues with notifications or rendering, run the diagnostic suite:

```bash
gh orbit doctor
```

---

## 📄 License

MIT License. See [LICENSE](LICENSE) for details.

*For technical depth and contribution guides, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).*
