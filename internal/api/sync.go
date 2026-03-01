package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
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
func (s *SyncEngine) Sync() error {
	user, err := s.client.CurrentUser()
	if err != nil {
		return err
	}

	userID := strconv.FormatInt(user.ID, 10)
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
		opts := api.ReplacePathOptions{
			Method: http.MethodGet,
			Path:   path,
		}

		if meta.LastModified != "" {
			opts.Headers = map[string]string{"If-Modified-Since": meta.LastModified}
		}
		if meta.ETag != "" {
			if opts.Headers == nil {
				opts.Headers = make(map[string]string)
			}
			opts.Headers["If-None-Match"] = meta.ETag
		}

		var page []GHNotification
		resp, err := s.client.rest.Do(opts.Method, opts.Path, nil, &page)
		if err != nil {
			// Handle 304 Not Modified
			if restErr, ok := err.(*api.HTTPError); ok && restErr.StatusCode == http.StatusNotModified {
				return nil, &newMeta, nil
			}
			return nil, nil, err
		}

		allNotifications = append(allNotifications, page...)

		// Update metadata from headers of the first page
		if path == "notifications" || path == "notifications?per_page=100" {
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
			links := api.ParseLinkHeader(linkHeader)
			if next, ok := links["next"]; ok {
				path = next
			}
		}
	}

	return allNotifications, &newMeta, nil
}
