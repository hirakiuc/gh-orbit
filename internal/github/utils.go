package github

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
)

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
