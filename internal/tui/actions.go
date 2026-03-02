package tui

import (
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
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

	switch notif.SubjectType {
	case "PullRequest":
		number := extractNumberFromURL(notif.SubjectURL)
		if number != "" {
			m.status = "Opening PR..."
			return m.ViewPRWeb(repo, number)
		}
	case "Issue":
		number := extractNumberFromURL(notif.SubjectURL)
		if number != "" {
			m.status = "Opening issue..."
			return m.ViewIssueWeb(repo, number)
		}
	case "Release":
		tag := extractTagFromURL(notif.SubjectURL)
		if tag != "" {
			m.status = "Opening release..."
			return m.ViewReleaseWeb(repo, tag)
		}
	}

	// Fallback to standard browser open
	m.status = "Opening in browser..."
	return m.OpenBrowser(notif.HTMLURL)
}

// OpenBrowser opens the given URL in the default browser.
func (m *Model) OpenBrowser(url string) tea.Cmd {
	m.logger.Info("opening browser", "url", url)
	if !isValidGitHubURL(url) {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("refusing to open untrusted URL: %s", url)}
		}
	}

	return func() tea.Msg {
		b := browser.New("", nil, nil)
		if err := b.Browse(url); err != nil {
			m.logger.Error("failed to open browser", "error", err)
			return errMsg{err: err}
		}
		return actionCompleteMsg{}
	}
}

// CheckoutPR executes 'gh pr checkout' for the given repo and PR number.
func (m *Model) CheckoutPR(repo, number string) tea.Cmd {
	m.logger.Info("checking out PR", "repo", repo, "number", number)
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

	// #nosec G204: PR number and repo name are strictly regex-validated above
	c := exec.Command("gh", "pr", "checkout", number, "-R", repo)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			m.logger.Error("checkout failed", "error", err)
			return errMsg{err: err}
		}
		m.logger.Info("checkout successful", "repo", repo, "number", number)
		return actionCompleteMsg{}
	})
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
	m.logger.Info("executing gh view", "command", ghCmd, "repo", repo, "arg", arg)

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

	return func() tea.Msg {
		// #nosec G204: all parameters are strictly regex-validated above
		c := exec.Command("gh", ghCmd, "view", arg, "-R", repo, "--web")
		if err := c.Run(); err != nil {
			m.logger.Error("gh view command failed", "command", ghCmd, "error", err)
			return errMsg{err: err}
		}
		return actionCompleteMsg{}
	}
}

// URL extraction helpers

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
	// Note: API for releases usually has a numeric ID at the end, but 'gh release view' prefers tags.
	// However, 'gh release view' also accepts numeric IDs if provided.
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if reTagName.MatchString(last) {
			return last
		}
	}
	return ""
}
