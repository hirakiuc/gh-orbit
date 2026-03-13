package tui

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	rePRNumber = regexp.MustCompile(`^[0-9]+$`)
	reRepoName = regexp.MustCompile(`^[a-zA-Z0-9-._]+/[a-zA-Z0-9-._]+$`)
	reTagName  = regexp.MustCompile(`^[a-zA-Z0-9-._/]+$`)
)

func extractNumberFromURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	// Example: https://api.github.com/repos/owner/repo/pulls/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if rePRNumber.MatchString(last) {
			return last
		}
	}
	return ""
}

func extractTagFromURL(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}

	// Example: https://api.github.com/repos/owner/repo/releases/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if reTagName.MatchString(last) {
			return last
		}
	}
	return ""
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
