package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
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

	switch a := action.(type) {
	case ActionQuit:
		return tea.Quit
	case ActionShowToast:
		return i.model.ui.SetToast(a.Message)
	case ActionSyncNotifications:
		return i.model.syncNotificationsWithForce(a.Force)
	case ActionMarkRead:
		return i.model.MarkReadByID(a.ID, a.Read)
	case ActionSetPriority:
		return i.model.setPriorityByID(a.ID, a.Priority)
	case ActionViewWeb:
		return i.model.ViewItem(item{notification: a.Notification})
	case ActionCheckoutPR:
		return i.model.CheckoutPR(a.Repository, a.Number)
	case ActionEnrichItems:
		return i.model.enrichItems(a.Notifications)
	case ActionLoadNotifications:
		return i.model.loadNotifications()
	case ActionUpdateRateLimit:
		return func() tea.Msg {
			// #nosec G115: standard conversion
			i.model.traffic.UpdateRateLimit(context.Background(), a.Remaining)
			return nil
		}
	case ActionScheduleTick:
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
