package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// stripANSI is a simple utility to remove ANSI codes for content-based assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inANSI := false
	for _, r := range s {
		if r == '\x1b' {
			inANSI = true
			continue
		}
		if inANSI {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inANSI = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// executeCmd is a helper to recursively execute tea.Cmd and tea.BatchMsg in tests.
func executeCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, subCmd := range batch {
			_ = executeCmd(subCmd)
		}
		return nil
	}
	return msg
}
