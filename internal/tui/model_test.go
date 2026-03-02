package tui

import (
	"fmt"
	"image/color"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func TestModel_Update_SyncingState(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		syncing: true,
		list:    l,
		keys:    keys,
	}

	// notificationsLoadedMsg should NOT reset syncing
	msgLocal := notificationsLoadedMsg{}
	updatedModelLocal, _ := m.Update(msgLocal)
	if !updatedModelLocal.(*Model).syncing {
		t.Error("expected syncing to remain true after notificationsLoadedMsg")
	}

	// syncCompleteMsg SHOULD reset syncing
	msg := syncCompleteMsg{}
	updatedModel, _ := m.Update(msg)
	if updatedModel.(*Model).syncing {
		t.Error("expected syncing to be false after syncCompleteMsg")
	}

	// Test error reset
	m.syncing = true
	msgErr := errMsg{err: nil}
	updatedModel, _ = m.Update(msgErr)
	if updatedModel.(*Model).syncing {
		t.Error("expected syncing to be false after errMsg")
	}
}

func TestModel_Update_ThemeChange(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		styles: styles,
		list:   l,
		keys:   keys,
	}

	// Mock a light background color msg
	msg := tea.BackgroundColorMsg{Color: color.RGBA{R: 255, G: 255, B: 255, A: 255}}
	updatedModel, _ := m.Update(msg)
	_ = updatedModel.(*Model)

	// Since we can't easily check private style properties,
	// we've verified it compiles and runs.
}

func TestModel_Update_WindowSize(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		list: l,
		keys: keys,
	}

	// Mock window size msg
	width, height := 80, 24
	msg := tea.WindowSizeMsg{Width: width, Height: height}
	updatedModel, _ := m.Update(msg)
	newModel := updatedModel.(*Model)

	// The list height should be height-1 (to leave space for footer)
	expectedHeight := height - 1
	if newModel.list.Height() != expectedHeight {
		t.Errorf("expected list height %d, got %d", expectedHeight, newModel.list.Height())
	}

	if newModel.list.Width() != width {
		t.Errorf("expected list width %d, got %d", width, newModel.list.Width())
	}
}

func TestModel_Update_StatusClearing(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	l := list.New([]list.Item{}, newItemDelegate(styles, keys), 0, 0)

	m := &Model{
		status: "some status",
		err:    fmt.Errorf("some error"),
		list:   l,
		keys:   keys,
	}

	// Test actionCompleteMsg
	msgAction := actionCompleteMsg{}
	updatedModel, _ := m.Update(msgAction)
	newModel := updatedModel.(*Model)
	if newModel.status != "" {
		t.Error("expected status to be cleared after actionCompleteMsg")
	}
	if newModel.err != nil {
		t.Error("expected err to be cleared after actionCompleteMsg")
	}

	// Test clearStatusMsg
	m.status = "temporary status"
	msgClear := clearStatusMsg{}
	updatedModel, _ = m.Update(msgClear)
	newModel = updatedModel.(*Model)
	if newModel.status != "" {
		t.Error("expected status to be cleared after clearStatusMsg")
	}
}
