package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hirakiuc/gh-orbit/internal/db"
)

// NotificationFetcher implements the Fetcher interface for the GitHub REST API.
type NotificationFetcher struct {
	client *Client
}

func NewNotificationFetcher(client *Client) *NotificationFetcher {
	return &NotificationFetcher{client: client}
}

func (f *NotificationFetcher) FetchNotifications(meta *db.SyncMeta) ([]GHNotification, *db.SyncMeta, error) {
	var allNotifications []GHNotification
	newMeta := *meta

	path := "notifications?per_page=100"

	// Only fetch all notifications on first sync
	if meta.LastModified != "" || meta.ETag != "" {
		path = "notifications"
	}

	for path != "" {
		url := path
		if !strings.HasPrefix(url, "http") {
			url = f.client.BaseURL() + path
		}

		req, err := http.NewRequest(http.MethodGet, url, nil) // #nosec G704: Trusted GitHub API URLs
		if err != nil {
			return nil, nil, err
		}

		if meta.LastModified != "" {
			req.Header.Set("If-Modified-Since", meta.LastModified)
		}
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}

		resp, err := f.client.HTTP().Do(req) // #nosec G704: Trusted GitHub API URLs
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusNotModified {
			return nil, &newMeta, nil
		}

		if resp.StatusCode >= 400 {
			return nil, nil, fmt.Errorf("API error: %s", resp.Status)
		}

		var page []GHNotification
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, nil, err
		}

		allNotifications = append(allNotifications, page...)

		// Update metadata from headers
		if strings.Contains(url, "notifications") {
			if lm := resp.Header.Get("Last-Modified"); lm != "" {
				newMeta.LastModified = lm
			}
			if et := resp.Header.Get("ETag"); et != "" {
				newMeta.ETag = et
			}
			if pi := resp.Header.Get("X-Poll-Interval"); pi != "" {
				if interval, err := strconv.Atoi(pi); err == nil {
					newMeta.PollInterval = interval
				}
			}
		}

		// Handle pagination
		path = ""
		if linkHeader := resp.Header.Get("Link"); linkHeader != "" {
			links := parseLinkHeader(linkHeader)
			if next, ok := links["next"]; ok {
				path = next
			}
		}
	}

	return allNotifications, &newMeta, nil
}

func parseLinkHeader(header string) map[string]string {
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
