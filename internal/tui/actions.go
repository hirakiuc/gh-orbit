package tui

import (
	"fmt"
	"os/exec"
	"regexp"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
)

var (
	rePRNumber = regexp.MustCompile(`^[0-9]+$`)
	reRepoName = regexp.MustCompile(`^[a-zA-Z0-9-._]+/[a-zA-Z0-9-._]+$`)
)

// OpenBrowser opens the given URL in the default browser.
func (m *Model) OpenBrowser(url string) tea.Cmd {
	if !isValidGitHubURL(url) {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("refusing to open untrusted URL: %s", url)}
		}
	}

	return func() tea.Msg {
		b := browser.New("", nil, nil)
		if err := b.Browse(url); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
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

	// #nosec G204: PR number and repo name are strictly regex-validated above
	c := exec.Command("gh", "pr", "checkout", number, "-R", repo)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return errMsg{err: err}
		}
		return nil
	})
}

// ViewPRWeb executes 'gh pr view --web' for the given repo and PR number.
func (m *Model) ViewPRWeb(repo, number string) tea.Cmd {
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

	return func() tea.Msg {
		// #nosec G204: PR number and repo name are strictly regex-validated above
		c := exec.Command("gh", "pr", "view", number, "-R", repo, "--web")
		if err := c.Run(); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}
