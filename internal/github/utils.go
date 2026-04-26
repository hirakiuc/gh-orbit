package github

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

var (
	RePRNumber = regexp.MustCompile(`^[0-9]+$`)
	ReTagName  = regexp.MustCompile(`^[a-zA-Z0-9-._/]+$`)
	ReRepoName = regexp.MustCompile(`^[a-zA-Z0-9-._]+/[a-zA-Z0-9-._]+$`)
)

// ExtractNumberFromURL parses the last segment of a GitHub API URL as a number (Issue/PR).
func ExtractNumberFromURL(u string) string {
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
		if RePRNumber.MatchString(last) {
			return last
		}
	}
	return ""
}

// ExtractTagFromURL parses the last segment of a GitHub API URL as a tag (Release).
func ExtractTagFromURL(u string) string {
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
		if ReTagName.MatchString(last) {
			return last
		}
	}
	return ""
}

// ExtractOwnerRepoFromURL extracts owner and repo from a GitHub API URL.
func ExtractOwnerRepoFromURL(u string) (string, string) {
	if u == "" {
		return "", ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", ""
	}

	// Example: https://api.github.com/repos/owner/repo/pulls/123
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) >= 3 && segments[0] == "repos" {
		return segments[1], segments[2]
	}
	return "", ""
}

// ParseRateLimitInfo extracts GitHub-specific rate limit headers into a standard structure.
func ParseRateLimitInfo(h http.Header) models.RateLimitInfo {
	info := models.RateLimitInfo{
		Limit:     5000, // Default assume healthy
		Remaining: 5000,
		Resource:  h.Get("X-RateLimit-Resource"),
	}

	if val := h.Get("X-RateLimit-Limit"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			info.Limit = i
		}
	}
	if val := h.Get("X-RateLimit-Remaining"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			info.Remaining = i
		}
	}
	if val := h.Get("X-RateLimit-Used"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			info.Used = i
		}
	}
	if val := h.Get("X-RateLimit-Reset"); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			info.Reset = time.Unix(i, 0)
		}
	}
	if val := h.Get("Retry-After"); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			info.RetryAfter = time.Duration(i) * time.Second
		} else if t, err := http.ParseTime(val); err == nil {
			info.RetryAfter = time.Until(t)
			if info.RetryAfter < 0 {
				info.RetryAfter = 0
			}
		}
	}

	return info
}

func ParseLinkHeader(header string) map[string]string {
	links := make(map[string]string)
	for _, link := range strings.Split(header, ",") {
		segments := strings.Split(strings.TrimSpace(link), ";")
		if len(segments) < 2 {
			continue
		}

		url := strings.Trim(segments[0], "<>")
		for _, segment := range segments[1:] {
			parts := strings.Split(strings.TrimSpace(segment), "=")
			if len(parts) != 2 || strings.TrimSpace(parts[0]) != "rel" {
				continue
			}
			rel := strings.Trim(parts[1], "\"")
			links[rel] = url
		}
	}
	return links
}

// MapHTTPError converts an HTTP status code into a standard internal sentinel error.
func MapHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent, http.StatusNotModified:
		return nil
	case http.StatusUnauthorized:
		return types.ErrUnauthorized
	case http.StatusForbidden, http.StatusTooManyRequests:
		return types.ErrRateLimited
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return types.ErrInternalServerError
	default:
		return fmt.Errorf("github: unexpected status code %d", statusCode)
	}
}
