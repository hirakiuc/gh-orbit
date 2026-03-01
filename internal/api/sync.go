package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/db"
)

const DefaultPollInterval = 60 // seconds

// SyncEngine orchestrates the synchronization of notifications.
type SyncEngine struct {
	client *Client
	db     *db.DB
}

func NewSyncEngine(client *Client, database *db.DB) *SyncEngine {
	return &SyncEngine{
		client: client,
		db:     database,
	}
}

// Sync performs a full synchronization cycle for notifications.
func (s *SyncEngine) Sync(userID string) error {
	metaKey := "notifications"

	meta, err := s.db.GetSyncMeta(userID, metaKey)
	if err != nil {
		return err
	}

	// Initialize meta if not exists
	if meta == nil {
		meta = &db.SyncMeta{
			UserID:       userID,
			Key:          metaKey,
			PollInterval: DefaultPollInterval,
		}
	}

	// Check if we should poll based on LastSyncAt and PollInterval
	if time.Since(meta.LastSyncAt).Seconds() < float64(meta.PollInterval) {
		return nil // Too soon to poll
	}

	notifications, newMeta, err := s.fetchNotifications(meta)
	if err != nil {
		meta.LastError = err.Error()
		meta.LastErrorAt = time.Now()
		_ = s.db.UpdateSyncMeta(*meta)
		return err
	}

	// If 304 Not Modified, notifications will be empty but newMeta might have updated PollInterval
	if len(notifications) > 0 {
		for _, n := range notifications {
			err := s.db.UpsertNotification(db.Notification{
				GitHubID:           n.ID,
				SubjectTitle:       n.Subject.Title,
				SubjectType:        n.Subject.Type,
				Reason:             n.Reason,
				RepositoryFullName: n.Repository.FullName,
				HTMLURL:            "", // Will be enriched in later phases if needed
				UpdatedAt:          n.UpdatedAt,
			})
			if err != nil {
				return fmt.Errorf("failed to save notification %s: %w", n.ID, err)
			}
		}
	}

	newMeta.LastSyncAt = time.Now()
	newMeta.LastError = "" // Clear previous error on success
	return s.db.UpdateSyncMeta(*newMeta)
}

func (s *SyncEngine) fetchNotifications(meta *db.SyncMeta) ([]GHNotification, *db.SyncMeta, error) {
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
			url = s.client.BaseURL() + path
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

		resp, err := s.client.HTTP().Do(req) // #nosec G704: Trusted GitHub API URLs
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

		// Update metadata from headers of the first page
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
	// Example: <https://api.github.com/user/repos?page=3&per_page=100>; rel="next",
	//          <https://api.github.com/user/repos?page=50&per_page=100>; rel="last"
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
