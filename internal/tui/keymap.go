package tui

import "charm.land/bubbles/v2/key"

// KeyMap defines the custom keybindings for the gh-orbit TUI.
type KeyMap struct {
	Sync            key.Binding
	SetPriorityLow  key.Binding
	SetPriorityMed  key.Binding
	SetPriorityHigh key.Binding
	CopyURL         key.Binding
	CheckoutPR      key.Binding
	ViewContextual  key.Binding
	OpenBrowser     key.Binding
}

// DefaultKeyMap returns the default keybindings for the application.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Sync: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "sync"),
		),
		SetPriorityLow: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "priority low"),
		),
		SetPriorityMed: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "priority med"),
		),
		SetPriorityHigh: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "priority high"),
		),
		CopyURL: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "copy url"),
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
	}
}

// ShortHelp returns the keybindings to be displayed in the mini help view.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Sync,
		k.OpenBrowser,
		k.ViewContextual,
	}
}

// FullHelp returns the keybindings to be displayed in the expanded help view.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Sync, k.CopyURL, k.OpenBrowser, k.ViewContextual},
		{k.SetPriorityLow, k.SetPriorityMed, k.SetPriorityHigh},
		{k.CheckoutPR},
	}
}
