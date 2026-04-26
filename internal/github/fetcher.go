package github

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
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

	// We always use all=true to ensure cross-device consistency
	path := "notifications?per_page=100&all=true"
	isFirstPage := true

	for path != "" {
		url := f.buildURL(path)
		req, err := f.prepareRequest(ctx, url, meta, force)
		if err != nil {
			return nil, nil, rlInfo, err
		}

		resp, err := f.client.HTTP().Do(req) // #nosec G704: Trusted GitHub API URLs
		if err != nil {
			return nil, nil, rlInfo, err
		}
		defer func() { _ = resp.Body.Close() }()

		// 1. Process Metadata & Rate Limits
		rlInfo = ParseRateLimitInfo(resp.Header)
		f.client.ReportRateLimit(rlInfo)

		// 2. Handle API Status
		if resp.StatusCode == http.StatusNotModified {
			f.logger.DebugContext(ctx, "sync: 304 Not Modified received", "url", url)
			return nil, &newMeta, rlInfo, nil
		}

		if err := f.handleResponseError(resp, rlInfo); err != nil {
			return nil, nil, rlInfo, err
		}

		// 3. Parse Data
		page, err := f.parsePage(resp)
		if err != nil {
			return nil, nil, rlInfo, err
		}
		allNotifications = append(allNotifications, page...)

		// 4. Update Sync Metadata (Only for the first page to capture the newest state)
		if isFirstPage {
			f.updateSyncMeta(&newMeta, resp, url)
			f.logger.DebugContext(ctx, "sync: updated checkpoint from first page", "etag", newMeta.ETag, "last_modified", newMeta.LastModified)
		}

		// 5. Handle Pagination
		path = f.getNextPagePath(resp)
		isFirstPage = false
	}

	span.SetAttributes(attribute.Int("notification_count", len(allNotifications)))
	return allNotifications, &newMeta, rlInfo, nil
}

func (f *NotificationFetcher) buildURL(path string) string {
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
	return url
}

func (f *NotificationFetcher) prepareRequest(ctx context.Context, url string, meta *models.SyncMeta, force bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	// Only apply conditional headers if NOT forcing a cold refresh
	if !force {
		if meta.LastModified != "" {
			req.Header.Set("If-Modified-Since", meta.LastModified)
		}
		if meta.ETag != "" {
			req.Header.Set("If-None-Match", meta.ETag)
		}
	}

	return req, nil
}

func (f *NotificationFetcher) handleResponseError(resp *http.Response, rl models.RateLimitInfo) error {
	err := MapHTTPError(resp.StatusCode)
	if err == nil {
		return nil
	}

	if errors.Is(err, types.ErrRateLimited) {
		return &models.RateLimitError{
			Resource:   rl.Resource,
			RetryAfter: rl.RetryAfter,
		}
	}

	return err
}

func (f *NotificationFetcher) parsePage(resp *http.Response) ([]Notification, error) {
	var rawPage []struct {
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

	if err := json.NewDecoder(resp.Body).Decode(&rawPage); err != nil {
		return nil, err
	}

	notifications := make([]Notification, len(rawPage))
	for i, p := range rawPage {
		notifications[i] = Notification{
			ID:        p.ID,
			Reason:    p.Reason,
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
		}
	}
	return notifications, nil
}

func (f *NotificationFetcher) updateSyncMeta(meta *models.SyncMeta, resp *http.Response, url string) {
	if resp.StatusCode != http.StatusOK || !strings.Contains(url, "notifications") {
		return
	}

	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		meta.LastModified = lm
	}
	if et := resp.Header.Get("ETag"); et != "" {
		if et != `W/""` {
			meta.ETag = et
		}
	}
	if pi := resp.Header.Get("X-Poll-Interval"); pi != "" {
		if interval, err := strconv.Atoi(pi); err == nil {
			meta.PollInterval = interval
		}
	}
}

func (f *NotificationFetcher) getNextPagePath(resp *http.Response) string {
	if linkHeader := resp.Header.Get("Link"); linkHeader != "" {
		links := ParseLinkHeader(linkHeader)
		if next, ok := links["next"]; ok {
			return next
		}
	}
	return ""
}
