package tui

import "strings"

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
