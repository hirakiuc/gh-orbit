# Vision: gh-orbit

## The Era of High-Volume Notification Noise

As development speed increases through AI-accelerated coding (Gemini, Claude, GitHub Copilot), the volume of Pull Requests, reviews, and CI notifications has reached a tipping point. Engineers are increasingly buried under a "noise floor" of team-wide notifications, making it difficult to identify critical items that require immediate attention.

**`gh-orbit`** was conceived to solve this problem by providing a high-performance "Control Center" for your GitHub notification lifecycle.

---

## 1. The Local-First Philosophy

`gh-orbit` is built on the principle that **Triage should be instant and offline-capable.**
By mirroring your notification stream into a private, local SQLite database, the extension allows you to:

- Assign priority and mute threads without the latency of network calls.
- Maintain a local "source of truth" for your triage state that persists across devices and sessions.
- Perform bulk operations (like marking all seen items as read) in milliseconds.

## 2. Security as a First-Class Citizen

In a modern security landscape, your notification metadata is sensitive. `gh-orbit` adheres to strict industrial standards:

- **Zero-Config Security**: No manual token management; it inherits the security context of your `gh` CLI.
- **Strict Isolation**: Local storage is locked down with `0700/0600` permissions, ensuring your data is private to your Unix user.
- **Traceable Reliability**: Every action is traced via OpenTelemetry, providing high-fidelity logs for troubleshooting without compromising privacy.

## 3. The Road to Intelligence (Future)

Our vision for `gh-orbit` extends beyond simple list management. We are building the foundation for **AI-Assisted Triage**:

- **Semantic Summarization**: Hooking into AI CLI tools to provide 1-line summaries of PRs directly in the TUI.
- **Risk Scoring**: Automatically flagging high-risk diffs or complex review requests based on local analysis.
- **CI Debugger**: Automatically fetching failed CI logs and presenting AI-generated root-cause insights within the notification detail view.

---

*For current implementation details, see [.agent/implementation_plan.md](../.agent/implementation_plan.md).*
*For technical architecture, see [ARCHITECTURE.md](ARCHITECTURE.md).*
