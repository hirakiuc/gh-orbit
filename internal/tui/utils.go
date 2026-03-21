package tui

import (
	"net/url"
	"strings"

	"github.com/hirakiuc/gh-orbit/internal/github"
)

func extractNumberFromURL(u string) string {
	return github.ExtractNumberFromURL(u)
}

func extractTagFromURL(u string) string {
	return github.ExtractTagFromURL(u)
}

func isValidGitHubURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.Host == "github.com" || strings.HasSuffix(parsed.Host, ".github.com")
}
