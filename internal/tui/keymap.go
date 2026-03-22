package tui

import (
	"charm.land/bubbles/v2/key"
	"github.com/hirakiuc/gh-orbit/internal/config"
)

// KeyMap defines the custom keybindings for the gh-orbit TUI.
type KeyMap struct {
	Sync             key.Binding
	PriorityUp       key.Binding
	PriorityDown     key.Binding
	PriorityNone     key.Binding
	Tab1             key.Binding
	Tab2             key.Binding
	Tab3             key.Binding
	Tab4             key.Binding
	CopyURL          key.Binding
	ToggleRead       key.Binding
	NextTab          key.Binding
	PrevTab          key.Binding
	CheckoutPR       key.Binding
	ViewContextual   key.Binding
	OpenBrowser      key.Binding
	ToggleDetail     key.Binding
	Back             key.Binding
	Quit             key.Binding
	FilterPR         key.Binding
	FilterIssue      key.Binding
	FilterDiscussion key.Binding
	Help             key.Binding
}

// NewKeyMap returns keybindings initialized from configuration.
func NewKeyMap(cfg *config.Config) KeyMap {
	k := cfg.Keys
	return KeyMap{
		Sync: key.NewBinding(
			key.WithKeys(k.Sync...),
			key.WithHelp(k.Sync[0], "sync"),
		),
		PriorityUp: key.NewBinding(
			key.WithKeys(k.PriorityUp...),
			key.WithHelp(k.PriorityUp[0], "priority up"),
		),
		PriorityDown: key.NewBinding(
			key.WithKeys(k.PriorityDown...),
			key.WithHelp(k.PriorityDown[0], "priority down"),
		),
		PriorityNone: key.NewBinding(
			key.WithKeys(k.PriorityNone...),
			key.WithHelp(k.PriorityNone[0], "clear priority"),
		),
		Tab1: key.NewBinding(
			key.WithKeys(k.Inbox...),
			key.WithHelp(k.Inbox[0], "inbox"),
		),
		Tab2: key.NewBinding(
			key.WithKeys(k.Unread...),
			key.WithHelp(k.Unread[0], "unread"),
		),
		Tab3: key.NewBinding(
			key.WithKeys(k.Triaged...),
			key.WithHelp(k.Triaged[0], "triaged"),
		),
		Tab4: key.NewBinding(
			key.WithKeys(k.All...),
			key.WithHelp(k.All[0], "all"),
		),
		CopyURL: key.NewBinding(
			key.WithKeys(k.CopyURL...),
			key.WithHelp(k.CopyURL[0], "copy url"),
		),
		ToggleRead: key.NewBinding(
			key.WithKeys(k.ToggleRead...),
			key.WithHelp(k.ToggleRead[0], "mark read/unread"),
		),
		NextTab: key.NewBinding(
			key.WithKeys(k.NextTab...),
			key.WithHelp(k.NextTab[0], "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys(k.PrevTab...),
			key.WithHelp(k.PrevTab[0], "prev tab"),
		),
		CheckoutPR: key.NewBinding(
			key.WithKeys(k.CheckoutPR...),
			key.WithHelp(k.CheckoutPR[0], "checkout pr"),
		),
		ViewContextual: key.NewBinding(
			key.WithKeys(k.ViewContextual...),
			key.WithHelp(k.ViewContextual[0], "view (gh cli)"),
		),
		OpenBrowser: key.NewBinding(
			key.WithKeys(k.OpenBrowser...),
			key.WithHelp(k.OpenBrowser[0], "open (browser)"),
		),
		ToggleDetail: key.NewBinding(
			key.WithKeys(k.ToggleDetail...),
			key.WithHelp(k.ToggleDetail[0], "peek detail"),
		),
		Back: key.NewBinding(
			key.WithKeys(k.Back...),
			key.WithHelp(k.Back[0], "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys(k.Quit...),
			key.WithHelp(k.Quit[0], "quit"),
		),
		FilterPR: key.NewBinding(
			key.WithKeys(k.FilterPR...),
			key.WithHelp(k.FilterPR[0], "filter PRs"),
		),
		FilterIssue: key.NewBinding(
			key.WithKeys(k.FilterIssue...),
			key.WithHelp(k.FilterIssue[0], "filter issues"),
		),
		FilterDiscussion: key.NewBinding(
			key.WithKeys(k.FilterDiscussion...),
			key.WithHelp(k.FilterDiscussion[0], "filter discussions"),
		),
		Help: key.NewBinding(
			key.WithKeys(k.Help...),
			key.WithHelp(k.Help[0], "help"),
		),
	}
}

// ShortHelp returns the keybindings to be displayed in the mini help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Help,
		k.Sync,
		k.ToggleRead,
		k.ToggleDetail,
		k.Quit,
	}
}

// FullHelp returns the keybindings to be displayed in the expanded help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Sync, k.CopyURL, k.OpenBrowser, k.ViewContextual},
		{k.ToggleDetail, k.Back, k.FilterPR, k.FilterIssue, k.FilterDiscussion},
		{k.PriorityUp, k.PriorityDown, k.PriorityNone},
		{k.Tab1, k.Tab2, k.Tab3, k.Tab4},
		{k.NextTab, k.PrevTab, k.CheckoutPR, k.Help, k.Quit},
	}
}
