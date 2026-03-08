package tui

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestRenderFooter(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	
	// 1. Syncing state
	m.ui.SetSyncing(true)
	f1 := stripANSI(m.renderFooter())
	assert.Contains(t, f1, "[NATIVE]") // default healthy bridge

	// 2. Error state
	m.err = fmt.Errorf("api error")
	view := m.View()
	// In v2, View returns a struct, so we access Content directly
	f2 := stripANSI(view.Content)
	assert.Contains(t, f2, "Error: api error")
	
	// 4. Other bridge states
	states := []struct {
		status   api.BridgeStatus
		expected string
	}{
		{api.StatusUnsupported, "[FALLBACK]"},
		{api.StatusPermissionsDenied, "[NO PERMS]"},
		{api.StatusBroken, "[BROKEN]"},
		{api.StatusUnknown, "[PROBING]"},
	}
	
	for _, st := range states {
		m.bridgeStatus = st.status
		f := stripANSI(m.renderFooter())
		assert.Contains(t, f, st.expected)
	}
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

func TestRenderMarkdown(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.updateMarkdownRenderer()
	
	md := "# Title\n\n- item 1\n- item 2"
	rendered := m.renderMarkdown(md)
	
	assert.NotEmpty(t, rendered)
	assert.Contains(t, stripANSI(rendered), "Title")
	assert.Contains(t, stripANSI(rendered), "item 1")
}

func TestRenderDetailView(t *testing.T) {
	m := newTestModel(t)
	m.width = 100
	m.height = 50
	
	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectTitle: "T1", SubjectURL: "u1"}},
	}
	m.listView.list.SetItems([]list.Item{item{notification: notifs[0]}})
	m.listView.list.Select(0)
	
	// 1. Loading state
	m.ui.SetFetching(true)
	v1 := stripANSI(m.renderDetailView())
	assert.Contains(t, v1, "Loading content...")
	
	// 2. Empty state
	m.ui.SetFetching(false)
	m.detailView.activeDetail = ""
	v2 := stripANSI(m.renderDetailView())
	assert.Contains(t, v2, "No description provided")
	
	// 3. Content state
	m.detailView.activeDetail = "Some description"
	v3 := stripANSI(m.renderDetailView())
	assert.Contains(t, v3, "Some description")
}
