package tui

import "charm.land/bubbles/v2/key"

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
}

// DefaultKeyMap returns the default keybindings for the application.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Sync: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "sync"),
		),
		PriorityUp: key.NewBinding(
			key.WithKeys("shift+up", "K"),
			key.WithHelp("shift+up", "priority up"),
		),
		PriorityDown: key.NewBinding(
			key.WithKeys("shift+down", "J"),
			key.WithHelp("shift+down", "priority down"),
		),
		PriorityNone: key.NewBinding(
			key.WithKeys("0"),
			key.WithHelp("0", "clear priority"),
		),
		Tab1: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "inbox"),
		),
		Tab2: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "unread"),
		),
		Tab3: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "triaged"),
		),
		Tab4: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "all"),
		),
		CopyURL: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy url"),
		),
		ToggleRead: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "mark read/unread"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("]", "tab"),
			key.WithHelp("]", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("[", "shift+tab"),
			key.WithHelp("[", "prev tab"),
		),
		CheckoutPR: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "checkout pr"),
		),
		ViewContextual: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "view (gh cli)"),
		),
		OpenBrowser: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open (browser)"),
		),
		ToggleDetail: key.NewBinding(
			key.WithKeys(" ", "space"),
			key.WithHelp("space", "peek detail"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc", "backspace"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		FilterPR: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "filter PRs"),
		),
		FilterIssue: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "filter issues"),
		),
		FilterDiscussion: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "filter discussions"),
		),
	}
}

// ShortHelp returns the keybindings to be displayed in the mini help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Sync,
		k.ToggleRead,
		k.ToggleDetail,
		k.Back,
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
		{k.NextTab, k.PrevTab, k.CheckoutPR, k.Quit},
	}
}
