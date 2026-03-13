package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Interpreter translates abstract Actions into Bubble Tea commands.
type Interpreter struct {
	model *Model
}

func NewInterpreter(m *Model) *Interpreter {
	return &Interpreter{model: m}
}

// Execute converts a single Action into its corresponding tea.Cmd.
func (i *Interpreter) Execute(action Action) tea.Cmd {
	if action == nil {
		return nil
	}

	if _, ok := action.(ActionQuit); ok {
		return tea.Quit
	}

	if a, ok := action.(ActionShowToast); ok {
		return i.model.ui.SetToast(a.Message)
	}

	if a, ok := action.(ActionSyncNotifications); ok {
		return i.model.syncNotificationsWithForce(a.Force)
	}

	if a, ok := action.(ActionMarkRead); ok {
		return i.model.MarkReadByID(a.ID, a.Read)
	}

	if a, ok := action.(ActionSetPriority); ok {
		return i.model.setPriorityByID(a.ID, a.Priority)
	}

	if a, ok := action.(ActionViewWeb); ok {
		return i.executeViewWeb(a.Notification)
	}

	if a, ok := action.(ActionOpenBrowser); ok {
		return i.executeOpenBrowser(a.URL)
	}

	if a, ok := action.(ActionCheckoutPR); ok {
		return i.executeCheckoutPR(a.Repository, a.Number)
	}

	if a, ok := action.(ActionEnrichItems); ok {
		return i.model.enrichItems(a.Notifications)
	}

	if a, ok := action.(ActionFetchDetail); ok {
		return i.model.FetchDetailCmd(a.ID, a.URL, a.SubjectType)
	}

	if _, ok := action.(ActionLoadNotifications); ok {
		return i.model.loadNotifications()
	}

	if a, ok := action.(ActionUpdateRateLimit); ok {
		return func() tea.Msg {
			i.model.RateLimit = a.Info
			i.model.traffic.UpdateRateLimit(context.Background(), a.Info)
			return nil
		}
	}

	if a, ok := action.(ActionScheduleTick); ok {
		switch a.TickType {
		case TickHeartbeat:
			return i.model.tickHeartbeat()
		case TickClock:
			return i.model.tickClock()
		case TickToast:
			return tea.Tick(a.Interval, func(_ time.Time) tea.Msg {
				return clearStatusMsg{}
			})
		case TickEnrich:
			return tea.Tick(a.Interval, func(_ time.Time) tea.Msg {
				return viewportEnrichMsg{}
			})
		}
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

	return i.model.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		i.model.logger.InfoContext(ctx, "opening browser", "url", u)
		b := browser.New("", nil, nil)
		if err := b.Browse(u); err != nil {
			i.model.logger.ErrorContext(ctx, "failed to open browser", "error", err)
			return types.ErrMsg{Err: err}
		}
		return actionCompleteMsg{}
	})
}

func (i *Interpreter) executeCheckoutPR(repo, number string) tea.Cmd {
	// Validation logic moved from actions.go
	if !reRepoName.MatchString(repo) || !rePRNumber.MatchString(number) {
		return func() tea.Msg {
			return types.ErrMsg{Err: fmt.Errorf("invalid checkout parameters: %s#%s", repo, number)}
		}
	}

	// Use background context for logging as this spans across TEA execution cycles
	i.model.logger.InfoContext(context.Background(), "checking out PR", "repo", repo, "number", number)

	checkoutCmd := i.model.executor.InteractiveGH(func(err error) tea.Msg {
		if err != nil {
			i.model.logger.ErrorContext(context.Background(), "checkout failed", "error", err)
			return types.ErrMsg{Err: err}
		}
		i.model.logger.InfoContext(context.Background(), "checkout successful", "repo", repo, "number", number)
		return actionCompleteMsg{}
	}, "pr", "checkout", number, "-R", repo)

	// Find the item to mark as read
	var selectedID string
	if item, ok := i.model.listView.list.SelectedItem().(item); ok {
		selectedID = item.notification.GitHubID
	}

	if selectedID != "" {
		return tea.Batch(checkoutCmd, i.model.MarkReadByID(selectedID, true))
	}
	return checkoutCmd
}

func (i *Interpreter) executeViewWeb(n types.NotificationWithState) tea.Cmd {
	repo := n.RepositoryFullName

	var cmd tea.Cmd
	var toast string
	switch n.SubjectType {
	case "PullRequest":
		number := extractNumberFromURL(n.SubjectURL)
		if number != "" {
			toast = "Opening PR..."
			cmd = i.executeGHView("pr", repo, number)
		}
	case "Issue":
		number := extractNumberFromURL(n.SubjectURL)
		if number != "" {
			toast = "Opening issue..."
			cmd = i.executeGHView("issue", repo, number)
		}
	case "Release":
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

	return tea.Batch(cmd, i.model.ui.SetToast(toast), i.model.MarkReadByID(n.GitHubID, true))
}

func (i *Interpreter) executeGHView(ghCmd, repo, arg string) tea.Cmd {
	// Validation
	if !reRepoName.MatchString(repo) {
		return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid repo: %s", repo)} }
	}
	if ghCmd == "release" {
		if !reTagName.MatchString(arg) {
			return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid tag: %s", arg)} }
		}
	} else if !rePRNumber.MatchString(arg) {
		return func() tea.Msg { return types.ErrMsg{Err: fmt.Errorf("invalid number: %s", arg)} }
	}

	return i.model.traffic.Submit(api.PriorityUser, func(ctx context.Context) tea.Msg {
		i.model.logger.InfoContext(ctx, "executing gh view", "command", ghCmd, "repo", repo, "arg", arg)
		if err := i.model.executor.Run(ctx, "gh", ghCmd, "view", arg, "-R", repo, "--web"); err != nil {
			i.model.logger.ErrorContext(ctx, "gh view command failed", "command", ghCmd, "error", err)
			return types.ErrMsg{Err: err}
		}
		return actionCompleteMsg{}
	})
}
