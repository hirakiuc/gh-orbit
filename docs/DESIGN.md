# Design Doc: gh-orbit

## 1. Introduction

**`gh-orbit`** is a GitHub CLI (`gh`) extension providing a TUI-based notification management, triage, and workflow control system. Designed for the era of AI-accelerated development, it helps engineers filter the noise of high-volume Pull Requests and notifications, using a local SQLite database and AI tool integration to keep important tasks in their immediate "Orbit."

## 2. Problem Statement

* **Authentication Overhead:** Managing Personal Access Tokens (PATs) is manual, repetitive, and poses security risks.
* **Notification Noise:** Team-wide review requests and bot notifications bury critical, individual action items.
* **Context Switching:** Jumping between the terminal and browser for initial triage breaks flow and reduces productivity.
* **Review Fatigue:** The surge of AI-generated PRs increases the cognitive load required to summarize and prioritize reviews.

## 3. Proposed Solution

Build a `gh` extension that consumes the GitHub Notifications API and structures data into a local SQLite DB. It will feature custom filtering logic, macOS native notifications, and a TUI interface designed to trigger local AI analysis (e.g., Ollama) before the user even opens a browser.

---

## 4. System Architecture

* **Source:** GitHub Notifications API (REST).
* **Storage:** Local SQLite (for state, priority, and AI analysis cache).
* **Interface:** Bubble Tea (TUI) & macOS User Notifications.
* **Intelligence:** Hook-based integration with local LLM CLI tools.

---

## 5. Roadmap & Features

### Phase 1: Foundation (MVP)

* **Zero-Config Auth:** Inherit credentials from `gh auth token`.
* **Differential Sync:** Poll `GET /notifications` using `Last-Modified` headers to respect API rate limits.
* **Smart Notify:** Send macOS native notifications only for specific reasons (e.g., `review_requested`) based on a YAML config.
* **Basic Persistence:** Store notification metadata and "Seen" status in SQLite.

### Phase 2: Triage & Orbit Control

* **TUI Dashboard:** A high-performance list view using the Bubble Tea framework.
* **Local Triage:** Assign priorities (`1/2/3`) or "Mute" specific threads locally without affecting GitHub labels (unless configured).
* **Quick Preview:** View PR descriptions and CI status directly in the terminal.
* **Action Hub:** Launch `gh pr checkout` or `gh pr view --web` directly from the TUI.

### Phase 3: Intelligence Integration (Future)

* **Local AI Hook:** Trigger commands like `ollama` or `mods` to analyze PR diffs.
* **Pre-review Insights:** Display AI-generated 1-line summaries or risk scores in the TUI list.
* **CI Debugger:** Automatically fetch logs for failed CI notifications and pipe them to an AI for root-cause estimation.

---

## 6. Data Model (Proposed SQLite Schema)

### Table: `notifications`

* `id` (TEXT, PK): GitHub Notification ID.
* `subject_title` (TEXT): Title of the PR or Issue.
* `reason` (TEXT): Why the notification was sent.
* `repository` (TEXT): Full name of the repo.
* `updated_at` (DATETIME): Last update from GitHub.

### Table: `orbit_state`

* `notification_id` (FK): Links to `notifications`.
* `priority` (INTEGER): 0 (None) to 3 (High).
* `status` (TEXT): `entry`, `tracking`, `archived`.
* `is_read_locally` (BOOLEAN).

### Table: `intel_reports`

* `notification_id` (FK): Links to `notifications`.
* `category` (TEXT): `summary`, `review`, `ci_debug`.
* `content` (TEXT): Markdown output from AI tools.
* `status` (TEXT): `pending`, `success`, `failed`.

---

## 7. Implementation Details

* **Language:** Go (Golang).
* **TUI Framework:** [Charmbracelet Bubble Tea](https://github.com/charmbracelet/bubbletea).
* **SQLite Driver:** `modernc.org/sqlite` (CGO-free for easy cross-compilation).
* **Notification Utility:** `gen2brain/beeep` for cross-platform (macOS focused) alerts.

---

## 8. References

* [GitHub REST API: Notifications](https://docs.github.com/en/rest/activity/notifications)
* [GitHub CLI: Creating Extensions](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
