package tui

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/stretchr/testify/assert"
)

func TestUIController_Update(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)

	// 1. Toast clearing
	ui.toastMessage = "existing"
	ui, _ = ui.Update(clearStatusMsg{})
	assert.Empty(t, ui.toastMessage)

	// 2. Spinner tick (when active)
	ui.syncing = true
	_, cmd := ui.Update(spinner.TickMsg{})
	assert.NotNil(t, cmd)
}

func TestUIController_View(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)
	ui.SetSize(100, 20)

	// 1. Toast rendering
	ui.toastMessage = "alert!"
	v1 := ui.View("base", false, 0, 0, 0)
	assert.Contains(t, v1, "alert!")

	// 2. Filter chip rendering
	ui.resourceFilter = "PullRequest"
	v2 := ui.View("base", false, 0, 0, 0)
	assert.Contains(t, v2, "FILTER: PULLREQUEST")
}
