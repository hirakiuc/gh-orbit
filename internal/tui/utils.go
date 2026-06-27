package tui

import (
	"net/url"
	"strings"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
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

func extractReviewWorkspaceRepository(n triage.NotificationWithState) (types.ReviewWorkspaceRepository, bool) {
	parts := strings.Split(n.RepositoryFullName, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return types.ReviewWorkspaceRepository{}, false
	}

	host := "github.com"
	if parsed, err := url.Parse(n.HTMLURL); err == nil && parsed.Host != "" {
		host = parsed.Host
	}

	return types.ReviewWorkspaceRepository{
		Host:  host,
		Owner: parts[0],
		Name:  parts[1],
	}, true
}
