package tui

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Interpreter translates abstract Actions into Bubble Tea commands.
type Interpreter struct {
	model *Model
}

func NewInterpreter(m *Model) *Interpreter {
	return &Interpreter{model: m}
}

func (i *Interpreter) Execute(action Action) tea.Cmd {
	switch a := action.(type) {
	case nil:
		return nil
	case ActionQuit:
		return tea.Quit
	case ActionShowToast:
		return i.model.ui.SetToast(a.Message)
	case ActionSetSyncing:
		return i.model.ui.SetSyncing(a.Enabled)
	case ActionSetFetching:
		return i.model.ui.SetFetching(a.Enabled)
	case ActionSyncNotifications:
		return i.model.syncNotificationsWithForce(a.Force, a.IsManual)
	case ActionMarkRead:
		return i.model.MarkReadByID(a.ID, a.Read)
	case ActionSetPriority:
		return i.model.setPriorityByID(a.ID, a.Priority)
	case ActionViewWeb:
		return i.executeViewWeb(a.Notification)
	case ActionOpenBrowser:
		return i.executeOpenBrowser(a.URL)
	case ActionCheckoutPR:
		return i.executeCheckoutPR(a.NotificationID, a.Repository, a.Number)
	case ActionStartReviewWorkspace:
		return i.executeStartReviewWorkspace(a.Repository, a.PullRequestNumber)
	case ActionEnrichItems:
		return i.model.enrichItems(a.Notifications, a.Force)
	case ActionFetchDetail:
		return i.model.FetchDetailCmd(a.ID, a.URL, a.SubjectType, a.Force)
	case ActionLoadNotifications:
		return i.model.loadNotifications(a.IsInitial, a.IsForced, a.IsManual)
	case ActionUpdateRateLimit:
		return func() tea.Msg {
			i.model.RateLimit = a.Info
			// Connected mode intentionally delegates traffic control to the external engine.
			if i.model.traffic != nil {
				i.model.traffic.UpdateRateLimit(context.Background(), a.Info)
			}
			return nil
		}
	case ActionCheckFocusMode:
		return i.model.checkFocusMode()
	case ActionScheduleTick:
		return i.executeScheduleTick(a)
	default:
		return nil
	}
}

func (i *Interpreter) executeScheduleTick(action ActionScheduleTick) tea.Cmd {
	switch action.TickType {
	case TickHeartbeat:
		return i.model.tickHeartbeat()
	case TickClock:
		return i.model.tickClock()
	case TickToast:
		return tea.Tick(action.Interval, func(_ time.Time) tea.Msg {
			return clearStatusMsg{}
		})
	case TickEnrich:
		return i.model.tickEnrich()
	}
	return nil
}

func (i *Interpreter) executeOpenBrowser(u string) tea.Cmd {
	if u == "" {
		return nil
	}

	// Validation (previously in Model.OpenBrowser)
	if !isValidGitHubURL(u) {
		return func() tea.Msg {
			return types.ErrMsg{Err: fmt.Errorf("refusing to open untrusted URL: %s", u)}
		}
	}

	return i.model.submitTask("browser:"+u, 0, api.PriorityUser, func(ctx context.Context) any {
		i.model.logger.InfoContext(ctx, "opening browser", "url", u)
		b := browser.New("", nil, nil)
		if err := b.Browse(u); err != nil {
			i.model.logger.ErrorContext(ctx, "failed to open browser", "error", err)
			return types.ErrMsg{Err: err}
		}
		return actionCompleteMsg{}
	})
}

func (i *Interpreter) executeCheckoutPR(id, repo, number string) tea.Cmd {
	// Validation logic moved from actions.go
	if !github.ReRepoName.MatchString(repo) || !github.RePRNumber.MatchString(number) {
		return func() tea.Msg {
			return types.ErrMsg{Err: fmt.Errorf("invalid checkout parameters: %s#%s", repo, number)}
		}
	}

	// Use background context for logging as this spans across TEA execution cycles
	i.model.logger.InfoContext(context.Background(), "checking out PR", "repo", repo, "number", number)

	// Interactive command: must be handled at the TUI edge
	// #nosec G204: Intentional dynamic command execution for GitHub CLI
	checkoutCmd := tea.ExecProcess(exec.Command("gh", "pr", "checkout", number, "-R", repo), func(err error) tea.Msg {
		if err != nil {
			i.model.logger.ErrorContext(context.Background(), "checkout failed", "error", err)
			return types.ErrMsg{Err: err}
		}
		i.model.logger.InfoContext(context.Background(), "checkout successful", "repo", repo, "number", number)
		return actionCompleteMsg{}
	})

	if i.model.config.TUI.AutoReadOnOpen && id != "" {
		return tea.Batch(checkoutCmd, i.model.MarkReadByID(id, true))
	}
	return checkoutCmd
}

func (i *Interpreter) executeStartReviewWorkspace(repository types.ReviewWorkspaceRepository, pullRequestNumber int) tea.Cmd {
	if repository.Host == "" || repository.Owner == "" || repository.Name == "" || pullRequestNumber <= 0 {
		return func() tea.Msg {
			return types.ErrMsg{Err: types.ErrInvalidReviewWorkspaceRequest}
		}
	}

	return i.model.submitTask("review-workspace:start", 0, api.PriorityUser, func(ctx context.Context) any {
		err := i.model.backend.StartReviewWorkspace(ctx, types.ReviewWorkspaceStartRequest{
			Repository:        repository,
			PullRequestNumber: pullRequestNumber,
		})
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		return reviewWorkspaceStartedMsg{toast: "Starting review workspace..."}
	})
}

func (i *Interpreter) executeViewWeb(n triage.NotificationWithState) tea.Cmd {
	repo := n.RepositoryFullName

	var cmd tea.Cmd
	var toast string
	switch n.SubjectType {
	case triage.SubjectPullRequest:
		number := extractNumberFromURL(n.SubjectURL)
		if number != "" {
			toast = "Opening PR..."
			cmd = i.executeGHView("pr", repo, number)
		}
	case triage.SubjectIssue:
		number := extractNumberFromURL(n.SubjectURL)
		if number != "" {
			toast = "Opening Issue..."
			cmd = i.executeGHView("issue", repo, number)
		}

	case triage.SubjectRelease:
		tag := extractTagFromURL(n.SubjectURL)
		if tag != "" {
			toast = "Opening release..."
			cmd = i.executeGHView("release", repo, tag)
		}
	}

	if cmd == nil {
		// Fallback to standard browser open
		toast = "Opening in browser..."
		cmd = i.executeOpenBrowser(n.HTMLURL)
	}

	batch := []tea.Cmd{cmd, i.model.ui.SetToast(toast)}
	if i.model.config.TUI.AutoReadOnOpen {
		batch = append(batch, i.model.MarkReadByID(n.GitHubID, true))
	}

	return tea.Batch(batch...)
}

func (i *Interpreter) executeGHView(ghCmd, repo, arg string) tea.Cmd {
	// Validation
	if !github.ReRepoName.MatchString(repo) {
		return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid repo: %s", repo)} }
	}
	if ghCmd == "release" {
		if !github.ReTagName.MatchString(arg) {
			return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid tag: %s", arg)} }
		}
	} else if !github.RePRNumber.MatchString(arg) {
		return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid number: %s", arg)} }
	}

	return i.model.submitTask("gh-view:"+ghCmd+":"+repo+":"+arg, 0, api.PriorityUser, func(ctx context.Context) any {
		i.model.logger.InfoContext(ctx, "executing gh view", "command", ghCmd, "repo", repo, "arg", arg)
		if err := i.model.executor.Run(ctx, "gh", ghCmd, "view", arg, "-R", repo, "--web"); err != nil {
			i.model.logger.ErrorContext(ctx, "gh view command failed", "command", ghCmd, "error", err)
			return types.ErrMsg{Err: err}
		}
		return actionCompleteMsg{}
	})
}
