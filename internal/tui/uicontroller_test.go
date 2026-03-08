package tui

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/stretchr/testify/assert"
)

func TestUIController_SetToast(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)
	
	ui.SetToast("hello")
	assert.Equal(t, "hello", ui.toastMessage)
}

func TestUIController_Update(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)
	ui.SetSyncing(true)
	
	// Test spinner tick
	sMsg := spinner.TickMsg{}
	ui, cmd := ui.Update(sMsg)
	assert.NotNil(t, ui.spinner)
	assert.NotNil(t, cmd)
	
	// Test clear status
	ui.SetToast("test")
	ui, _ = ui.Update(clearStatusMsg{})
	assert.Empty(t, ui.toastMessage)
}

func TestUIController_View(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)
	ui.SetSize(100, 20)
	
	v := ui.View("base", false, 0, 0, 0)
	assert.Contains(t, v, "base")
	
	ui.SetToast("toast")
	ui.SetSyncing(true)
	v = ui.View("base", false, 0, 0, 0)
	assert.Contains(t, stripANSI(v), "toast")
	
	// Test scrollbar in detail view
	v = ui.View("base", true, 0.5, 10, 100)
	assert.NotEmpty(t, v)
	
	// Test filter chip
	ui.SetResourceFilter("PRs")
	v = ui.View("base", false, 0, 0, 0)
	assert.Contains(t, stripANSI(v), "FILTER: PRS")
}

func TestUIController_RenderSpinner(t *testing.T) {
	styles := DefaultStyles(true)
	ui := NewUIController(styles)
	
	assert.Empty(t, ui.RenderSpinner())
	
	ui.SetSyncing(true)
	assert.NotEmpty(t, ui.RenderSpinner())
	
	ui.SetSyncing(false)
	ui.SetFetching(true)
	assert.NotEmpty(t, ui.RenderSpinner())
}
