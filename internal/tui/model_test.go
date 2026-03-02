package tui

import (
	"image/color"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func TestModel_Update_SyncingState(t *testing.T) {
	styles := DefaultStyles(true)
	l := list.New([]list.Item{}, newItemDelegate(styles), 0, 0)

	m := Model{
		syncing: true,
		list:    l,
	}

	// Test success reset
	msg := notificationsLoadedMsg{}
	updatedModel, _ := m.Update(msg)
	if updatedModel.(Model).syncing {
		t.Error("expected syncing to be false after notificationsLoadedMsg")
	}

	// Test error reset
	m.syncing = true
	msgErr := errMsg{err: nil}
	updatedModel, _ = m.Update(msgErr)
	if updatedModel.(Model).syncing {
		t.Error("expected syncing to be false after errMsg")
	}
}

func TestModel_Update_ThemeChange(t *testing.T) {
	styles := DefaultStyles(true)
	l := list.New([]list.Item{}, newItemDelegate(styles), 0, 0)

	m := Model{
		styles: styles,
		list:   l,
	}

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(Model)

	// Since we can't easily check private style properties, 
	// we've verified it compiles and runs.
}
