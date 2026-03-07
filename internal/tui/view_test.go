package tui

import (
	"fmt"
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/stretchr/testify/assert"
)

func TestRenderFooter(t *testing.T) {
	styles := DefaultStyles(true)
	mockSync := mocks.NewMockSyncer(t)
	mockSync.EXPECT().BridgeStatus().Return("healthy").Maybe()

	m := Model{
		ui:     NewUIController(styles), 
		styles: styles,
		sync:   mockSync,
	}
	
	// 1. Syncing state
	m.ui.SetSyncing(true)
	f1 := stripANSI(m.renderFooter())
	assert.Contains(t, f1, "[NATIVE]")

	// 2. Error state
	m.err = fmt.Errorf("api error")
	view := m.View()
	// In v2, View returns a struct, so we access Content directly
	f2 := stripANSI(view.Content)
	assert.Contains(t, f2, "Error: api error")
}

func TestRenderHeader(t *testing.T) {
	styles := DefaultStyles(true)
	mockTraffic := mocks.NewMockTrafficController(t)
	mockTraffic.EXPECT().Remaining().Return(4999).Maybe()

	m := Model{
		styles:  styles,
		ui:      NewUIController(styles),
		traffic: mockTraffic,
		width:   80,
	}

	out := m.renderHeader()
	stripped := stripANSI(out)

	// Verify tabs are present
	assert.Contains(t, stripped, "Inbox")
	assert.Contains(t, stripped, "Unread")
	assert.Contains(t, stripped, "Rate: 4999")
}
