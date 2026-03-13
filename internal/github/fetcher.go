package github

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NotificationFetcher implements the Fetcher interface for the GitHub REST API.
type NotificationFetcher struct {
	client Client
	logger *slog.Logger
}

func NewNotificationFetcher(client Client, logger *slog.Logger) *NotificationFetcher {
	return &NotificationFetcher{
		client: client,
		logger: logger,
	}
}

func (f *NotificationFetcher) FetchNotifications(ctx context.Context, meta *models.SyncMeta, force bool) ([]Notification, *models.SyncMeta, models.RateLimitInfo, error) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "api.fetch_notifications",
		trace.WithAttributes(
			attribute.Bool("force", force),
			attribute.String("etag", meta.ETag),
		),
	)
	defer span.End()

	var allNotifications []Notification
	newMeta := *meta
	rlInfo := models.RateLimitInfo{Limit: 5000, Remaining: 5000}

	// True Cold Refresh: If force is true, we ignore all caching headers
	useConditional := !force && (meta.LastModified != "" || meta.ETag != "")

	// We always use all=true to ensure cross-device consistency
	path := "notifications?per_page=100&all=true"

	for path != "" {
		url := path
		if !strings.HasPrefix(url, "http") {
			url = f.client.BaseURL() + path
		}

		if !strings.Contains(url, "all=true") {
			if strings.Contains(url, "?") {
				url += "&all=true"
			} else {
				url += "?all=true"
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) // #nosec G704: Trusted GitHub API URLs
		if err != nil {
			return nil, nil, rlInfo, err
		}

		// Only apply conditional headers if NOT forcing a cold refresh
		if useConditional {
			if meta.LastModified != "" {
				req.Header.Set("If-Modified-Since", meta.LastModified)
			}
			if meta.ETag != "" {
				req.Header.Set("If-None-Match", meta.ETag)
			}
		}

		resp, err := f.client.HTTP().Do(req) // #nosec G704: Trusted GitHub API URLs
		if err != nil {
			return nil, nil, rlInfo, err
		}
		defer func() { _ = resp.Body.Close() }()

		f.logger.DebugContext(ctx, "received API response",
			"status", resp.StatusCode,
			"url", url,
			"force", force)

		// Update rate limit info if available
		rlInfo = ParseRateLimitInfo(resp.Header)
		f.client.ReportRateLimit(rlInfo)

		if resp.StatusCode == http.StatusNotModified {
			f.logger.DebugContext(ctx, "sync: 304 Not Modified received", "url", url)
			return nil, &newMeta, rlInfo, nil
		}

		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == 429 {
			return nil, nil, rlInfo, &models.RateLimitError{
				Resource:   rlInfo.Resource,
				RetryAfter: rlInfo.RetryAfter,
			}
		}

		if resp.StatusCode >= 400 {
			return nil, nil, rlInfo, fmt.Errorf("API error: %s", resp.Status)
		}

		var page []struct {
			ID         string    `json:"id"`
			Reason     string    `json:"reason"`
			UpdatedAt  time.Time `json:"updated_at"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Subject struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				Type   string `json:"type"`
				NodeID string `json:"node_id"`
			} `json:"subject"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			return nil, nil, rlInfo, err
		}

		for _, p := range page {
			allNotifications = append(allNotifications, Notification{
				ID: p.ID,
				Reason: p.Reason,
				UpdatedAt: p.UpdatedAt,
				Repository: struct {
					FullName string `json:"full_name"`
				}{FullName: p.Repository.FullName},
				Subject: struct {
					Title  string `json:"title"`
					URL    string `json:"url"`
					Type   string `json:"type"`
					NodeID string `json:"node_id"`
				}{
					Title:  p.Subject.Title,
					URL:    p.Subject.URL,
					Type:   p.Subject.Type,
					NodeID: p.Subject.NodeID,
				},
			})
		}

		// Update metadata from headers ONLY on successful data fetch (200 OK)
		if resp.StatusCode == http.StatusOK && strings.Contains(url, "notifications") {
			if lm := resp.Header.Get("Last-Modified"); lm != "" {
				newMeta.LastModified = lm
			}
			if et := resp.Header.Get("ETag"); et != "" {
				if et != `W/""` {
					newMeta.ETag = et
				}
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
			links := ParseLinkHeader(linkHeader)
			if next, ok := links["next"]; ok {
				path = next
			}
		}
	}

	span.SetAttributes(attribute.Int("notification_count", len(allNotifications)))
	return allNotifications, &newMeta, rlInfo, nil
}
