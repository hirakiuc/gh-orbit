package tui

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

var _ = slog.LevelInfo

var (
	rePRNumber = regexp.MustCompile(`^[0-9]+$`)
	reRepoName = regexp.MustCompile(`^[a-zA-Z0-9-._]+/[a-zA-Z0-9-._]+$`)
	reTagName  = regexp.MustCompile(`^[a-zA-Z0-9-._/]+$`)
)

// ViewItem determines the correct view action based on the notification subject type.
func (m *Model) ViewItem(i item) tea.Cmd {
	notif := i.notification
	repo := notif.RepositoryFullName

	var cmd tea.Cmd
	var toast string
	switch notif.SubjectType {
	case "PullRequest":
		number := extractNumberFromURL(notif.SubjectURL)
		if number != "" {
			toast = "Opening PR..."
			cmd = m.ViewPRWeb(repo, number)
		}
	case "Issue":
		number := extractNumberFromURL(notif.SubjectURL)
		if number != "" {
			toast = "Opening issue..."
			cmd = m.ViewIssueWeb(repo, number)
		}
	case "Release":
		tag := extractTagFromURL(notif.SubjectURL)
		if tag != "" {
			toast = "Opening release..."
			cmd = m.ViewReleaseWeb(repo, tag)
		}
	}

	if cmd == nil {
		// Fallback to standard browser open
		toast = "Opening in browser..."
		cmd = m.OpenBrowser(notif.HTMLURL)
	}

	return tea.Batch(cmd, m.ui.SetToast(toast), m.MarkRead(i))
}

// OpenBrowser opens the given URL in the default system browser.
func (m *Model) OpenBrowser(u string) tea.Cmd {
	if u == "" {
		return nil
	}
	if !isValidGitHubURL(u) {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("refusing to open untrusted URL: %s", u)}
		}
	}

	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		m.logger.InfoContext(ctx, "opening browser", "url", u)
		b := browser.New("", nil, nil)
		if err := b.Browse(u); err != nil {
			m.logger.ErrorContext(ctx, "failed to open browser", "error", err)
			return errMsg{err: err}
		}
		return actionCompleteMsg{}
	})
}

// CheckoutPR executes 'gh pr checkout' for the given repo and PR number.
func (m *Model) CheckoutPR(repo, number string) tea.Cmd {
	if !reRepoName.MatchString(repo) {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("invalid repository name: %s", repo)}
		}
	}
	if !rePRNumber.MatchString(number) {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("invalid PR number: %s", number)}
		}
	}

	// Find the item to mark as read
	var selectedItem item
	if i, ok := m.listView.list.SelectedItem().(item); ok {
		selectedItem = i
	}

	// Bubble Tea ExecProcess uses its own lifecycle, but we still log with context
	m.logger.InfoContext(context.Background(), "checking out PR", "repo", repo, "number", number)

	checkoutCmd := m.executor.InteractiveGH(func(err error) tea.Msg {
		if err != nil {
			m.logger.ErrorContext(context.Background(), "checkout failed", "error", err)
			return errMsg{err: err}
		}
		m.logger.InfoContext(context.Background(), "checkout successful", "repo", repo, "number", number)
		return actionCompleteMsg{}
	}, "pr", "checkout", number, "-R", repo)

	if selectedItem.notification.GitHubID != "" {
		return tea.Batch(checkoutCmd, m.MarkRead(selectedItem))
	}
	return checkoutCmd
}

// MarkReadByID marks a notification as read using only its ID.
func (m *Model) MarkReadByID(id string, read bool) tea.Cmd {
	// 1. Update master copy
	for idx, n := range m.allNotifications {
		if n.GitHubID == id {
			m.allNotifications[idx].IsReadLocally = read
			break
		}
	}

	m.applyFilters()

	// 2. Persistent Local & Remote Update via Traffic Controller
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		err := m.db.MarkReadLocally(ctx, id, read)
		if err != nil {
			m.logger.ErrorContext(ctx, "failed to update local read state", "error", err)
		}

		if read {
			err = m.client.MarkThreadAsRead(ctx, id)
			if err != nil {
				m.logger.ErrorContext(ctx, "failed to mark thread as read on GitHub", "error", err)
			}
		}
		return actionCompleteMsg{}
	})
}

// setPriorityByID updates the priority of a notification using only its ID.
func (m *Model) setPriorityByID(id string, priority int) tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		err := m.db.SetPriority(ctx, id, priority)
		if err != nil {
			return errMsg{err: err}
		}

		// Reload to reflect state
		notifs, err := m.db.ListNotifications(ctx)
		if err != nil {
			return errMsg{err: err}
		}

		toast := "Priority cleared"
		switch priority {
		case 1:
			toast = "Priority set to Low"
		case 2:
			toast = "Priority set to Medium"
		case 3:
			toast = "Priority set to High"
		}

		return priorityUpdatedMsg{notifications: notifs, toast: toast}
	})
}

const EnrichmentChunkSize = 10

// enrichItems triggers background enrichment for a specific set of notifications.
func (m *Model) enrichItems(toEnrich []types.NotificationWithState) tea.Cmd {
	if len(toEnrich) == 0 {
		return nil
	}

	// For a single item enrichment (Detail View), we use FetchDetail
	if len(toEnrich) == 1 {
		n := toEnrich[0]
		return m.FetchDetailCmd(n.GitHubID, n.SubjectURL, n.SubjectType)
	}

	// For multiple items (Viewport), split into smaller chunks to utilize concurrent workers
	var cmds []tea.Cmd

	for i := 0; i < len(toEnrich); i += EnrichmentChunkSize {
		end := i + EnrichmentChunkSize
		if end > len(toEnrich) {
			end = len(toEnrich)
		}
		chunk := toEnrich[i:end]

		cmds = append(cmds, m.traffic.Submit(api.PriorityEnrich, func(ctx context.Context) tea.Msg {
			results := m.enrich.FetchHybridBatch(ctx, chunk)
			if len(results) == 0 {
				return nil
			}

			notifs, err := m.db.ListNotifications(ctx)
			if err != nil {
				return errMsg{err: err}
			}
			return notificationsLoadedMsg{notifications: notifs, IsInitial: false}
		}))
	}

	return tea.Batch(cmds...)
}

func (m *Model) MarkRead(i item) tea.Cmd {
	return m.MarkReadByID(i.notification.GitHubID, true)
}

func (m *Model) ToggleRead(i item) tea.Cmd {
	return m.MarkReadByID(i.notification.GitHubID, !i.notification.IsReadLocally)
}


// ViewPRWeb executes 'gh pr view --web' for the given repo and PR number.
func (m *Model) ViewPRWeb(repo, number string) tea.Cmd {
	return m.ghViewCmd("pr", repo, number)
}

// ViewIssueWeb executes 'gh issue view --web' for the given repo and issue number.
func (m *Model) ViewIssueWeb(repo, number string) tea.Cmd {
	return m.ghViewCmd("issue", repo, number)
}

// ViewReleaseWeb executes 'gh release view --web' for the given repo and tag.
func (m *Model) ViewReleaseWeb(repo, tag string) tea.Cmd {
	return m.ghViewCmd("release", repo, tag)
}

// ghViewCmd executes a 'gh <cmd> view --web' command.
func (m *Model) ghViewCmd(ghCmd, repo, arg string) tea.Cmd {
	// Validation
	if !reRepoName.MatchString(repo) {
		return func() tea.Msg { return errMsg{err: fmt.Errorf("invalid repo: %s", repo)} }
	}
	if ghCmd == "release" {
		if !reTagName.MatchString(arg) {
			return func() tea.Msg { return errMsg{err: fmt.Errorf("invalid tag: %s", arg)} }
		}
	} else if !rePRNumber.MatchString(arg) {
		return func() tea.Msg { return errMsg{err: fmt.Errorf("invalid number: %s", arg)} }
	}

	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		m.logger.InfoContext(ctx, "executing gh view", "command", ghCmd, "repo", repo, "arg", arg)
		if err := m.executor.Run(ctx, "gh", ghCmd, "view", arg, "-R", repo, "--web"); err != nil {
			m.logger.ErrorContext(ctx, "gh view command failed", "command", ghCmd, "error", err)
			return errMsg{err: err}
		}
		return actionCompleteMsg{}
	})
}

// URL extraction helpers

func (m *Model) FetchDetailCmd(id, u, subjectType string) tea.Cmd {
	return m.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		res, err := m.enrich.FetchDetail(ctx, u, subjectType)
		if err != nil {
			return errMsg{err: err}
		}

		// Update database with granular enrich method
		err = m.db.EnrichNotification(ctx, id, res.Body, res.Author, res.HTMLURL, res.ResourceState)
		if err != nil {
			return errMsg{err: err}
		}

		return detailLoadedMsg{
			GitHubID:      id,
			Body:          res.Body,
			Author:        res.Author,
			HTMLURL:       res.HTMLURL,
			ResourceState: res.ResourceState,
		}
	})
}

func extractNumberFromURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	// Example: https://api.github.com/repos/owner/repo/pulls/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if rePRNumber.MatchString(last) {
			return last
		}
	}
	return ""
}

func extractTagFromURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	// Example: https://api.github.com/repos/owner/repo/releases/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if reTagName.MatchString(last) {
			return last
		}
	}
	return ""
}

func isValidGitHubURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.Host == "github.com" || strings.HasSuffix(parsed.Host, ".github.com")
}
