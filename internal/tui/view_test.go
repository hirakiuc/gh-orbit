package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
)

func TestRenderFooter(t *testing.T) {
	styles := DefaultStyles(true)

	m1 := Model{ui: NewUIController(styles), styles: styles}
	m1.ui.syncing = true
	f1 := stripANSI(m1.renderFooter())
	if !strings.Contains(f1, "Syncing") {
		t.Errorf("renderFooter() syncing state = %q, want it to contain %q", f1, "Syncing")
	}

	m2 := Model{ui: NewUIController(styles), styles: styles, err: fmt.Errorf("api error")}
	f2 := stripANSI(m2.renderFooter())
	if !strings.Contains(f2, "Error: api error") {
		t.Errorf("renderFooter() error state = %q, want it to contain %q", f2, "Error: api error")
	}

	m3 := Model{ui: NewUIController(styles), styles: styles, err: fmt.Errorf("api error")}
	m3.ui.syncing = true
	f3 := stripANSI(m3.renderFooter())
	if !strings.Contains(f3, "Syncing") || !strings.Contains(f3, "Error") {
		t.Errorf("renderFooter() combined state = %q, want it to contain both Syncing and Error", f3)
	}
}

func TestRenderList(t *testing.T) {
	styles := DefaultStyles(true)
	keys := DefaultKeyMap()
	delegate := newItemDelegate(styles, keys)
	l := list.New([]list.Item{}, delegate, 80, 20)

	m := Model{
		listView: ListModel{
			list: l,
		},
		styles: styles,
		ui:     NewUIController(styles),
	}

	out := m.renderList()
	stripped := stripANSI(out)

	// Verify tabs are present
	if !strings.Contains(stripped, "Inbox") {
		t.Errorf("renderList() should contain tab labels")
	}

	// Verify help content (short help should be at the bottom)
	if !strings.Contains(stripped, "sync") {
		t.Errorf("renderList() help should contain 'sync'")
	}
	if !strings.Contains(stripped, "back/close") {
		t.Errorf("renderList() help should contain 'back/close'")
	}
}

func TestRenderTabs(t *testing.T) {
	styles := DefaultStyles(true)
	m := Model{
		listView: ListModel{
			activeTab: 1, // Unread
		},
		styles: styles,
	}

	out := m.renderTabs()
	stripped := stripANSI(out)

	expectedTabs := []string{"Inbox", "Unread", "Triaged", "All"}
	for _, tab := range expectedTabs {
		if !strings.Contains(stripped, tab) {
			t.Errorf("renderTabs() = %q, want it to contain %q", stripped, tab)
		}
	}
}
