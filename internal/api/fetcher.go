package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps the GitHub REST and GQL API clients.
type Client struct {
	rest    *gh.RESTClient
	gql     *gh.GraphQLClient
	http    *http.Client
	host    string
	baseURL string
}

// NewClient initializes a new GitHub API client using go-gh.
func NewClient() (*Client, error) {
	host, _ := auth.DefaultHost()

	opts := gh.ClientOptions{
		Host:        host,
		EnableCache: true, // Enable ETag support for quota preservation
	}

	rest, err := gh.NewRESTClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	gql, err := gh.NewGraphQLClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create GQL client: %w", err)
	}

	httpClient, err := gh.NewHTTPClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	baseURL := "https://api.github.com/"
	if host != "github.com" {
		baseURL = fmt.Sprintf("https://%s/api/v3/", host)
	}

	return &Client{
		rest:    rest,
		gql:     gql,
		http:    httpClient,
		host:    host,
		baseURL: baseURL,
	}, nil
}

// CurrentUser retrieves the authenticated user's information.
func (c *Client) CurrentUser() (*GHUser, error) {
	var user GHUser
	err := c.rest.Get("user", &user)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current user: %w", err)
	}
	return &user, nil
}

// MarkThreadAsRead marks a single notification thread as read.
func (c *Client) MarkThreadAsRead(threadID string) error {
	path := fmt.Sprintf("notifications/threads/%s", threadID)
	return c.rest.Patch(path, nil, nil)
}

// REST returns the underlying REST client configured by go-gh.
func (c *Client) REST() *gh.RESTClient {
	return c.rest
}

// GQL returns the underlying GQL client configured by go-gh.
func (c *Client) GQL() *gh.GraphQLClient {
	return c.gql
}

// HTTP returns the underlying http.Client configured by go-gh.
func (c *Client) HTTP() *http.Client {
	return c.http
}

// BaseURL returns the GitHub API base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// NotificationFetcher implements the Fetcher interface for the GitHub REST API.
type NotificationFetcher struct {
	client GitHubClient
	logger *slog.Logger
}

func NewNotificationFetcher(client GitHubClient, logger *slog.Logger) *NotificationFetcher {
	return &NotificationFetcher{
		client: client,
		logger: logger,
	}
}

func (f *NotificationFetcher) FetchNotifications(ctx context.Context, meta *db.SyncMeta, force bool) ([]GHNotification, *db.SyncMeta, int, error) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "api.fetch_notifications",
		trace.WithAttributes(
			attribute.Bool("force", force),
			attribute.String("etag", meta.ETag),
		),
	)
	defer span.End()

	var allNotifications []GHNotification
	newMeta := *meta
	remainingRateLimit := 5000 // Default assume healthy

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
			return nil, nil, remainingRateLimit, err
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
			return nil, nil, remainingRateLimit, err
		}
		defer func() { _ = resp.Body.Close() }()

		f.logger.DebugContext(ctx, "received API response",
			"status", resp.StatusCode,
			"url", url,
			"force", force)

		// Update rate limit info if available
		if rl := resp.Header.Get("X-RateLimit-Remaining"); rl != "" {
			if remaining, err := strconv.Atoi(rl); err == nil {
				remainingRateLimit = remaining
			}
		}

		if resp.StatusCode == http.StatusNotModified {
			f.logger.DebugContext(ctx, "sync: 304 Not Modified received", "url", url)
			return nil, &newMeta, remainingRateLimit, nil
		}

		if resp.StatusCode >= 400 {
			return nil, nil, remainingRateLimit, fmt.Errorf("API error: %s", resp.Status)
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
			return nil, nil, remainingRateLimit, err
		}

		for _, p := range page {
			allNotifications = append(allNotifications, GHNotification{
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
			links := parseLinkHeader(linkHeader)
			if next, ok := links["next"]; ok {
				path = next
			}
		}
	}

	span.SetAttributes(attribute.Int("notification_count", len(allNotifications)))
	return allNotifications, &newMeta, remainingRateLimit, nil
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
