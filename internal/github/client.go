package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// ghClient wraps the GitHub REST and GQL API clients.
type ghClient struct {
	rest             *gh.RESTClient
	gql              *gh.GraphQLClient
	http             *http.Client
	host             string
	baseURL          string
	rateLimitUpdates chan models.RateLimitInfo
}

func (c *ghClient) SetRateLimitUpdates(ch chan models.RateLimitInfo) {
	c.rateLimitUpdates = ch
}

func (c *ghClient) ReportRateLimit(info models.RateLimitInfo) {
	if c.rateLimitUpdates == nil {
		return
	}
	select {
	case c.rateLimitUpdates <- info:
	default:
		// Drop update if channel is full or nobody is listening
	}
}

// NewClient initializes a new GitHub API client using go-gh.
func NewClient() (Client, error) {
	// Bypass auth for E2E testing
	if os.Getenv("GH_ORBIT_SKIP_AUTH") == "1" {
		return &ghClient{
			host:    "localhost",
			baseURL: os.Getenv("GH_ORBIT_API_URL"),
			http:    &http.Client{},
		}, nil
	}

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

	// Override for E2E testing
	if mockURL := os.Getenv("GH_ORBIT_API_URL"); mockURL != "" {
		baseURL = mockURL
	}

	return &ghClient{
		rest:    rest,
		gql:     gql,
		http:    httpClient,
		host:    host,
		baseURL: baseURL,
	}, nil
}

// NewTestClient creates a client specifically for unit testing with a mock server.
func NewTestClient(http *http.Client, baseURL string) Client {
	return &ghClient{
		http:    http,
		baseURL: baseURL,
	}
}

// CurrentUser retrieves the authenticated user's information.
func (c *ghClient) CurrentUser(ctx context.Context) (*User, error) {
	if os.Getenv("GH_ORBIT_SKIP_AUTH") == "1" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"user", nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req) // #nosec G704: Trusted E2E mock URL
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()

		c.ReportRateLimit(ParseRateLimitInfo(resp.Header))

		if err := MapHTTPError(resp.StatusCode); err != nil {
			if errors.Is(err, types.ErrRateLimited) {
				return nil, &models.RateLimitError{
					Resource:   "core",
					RetryAfter: ParseRateLimitInfo(resp.Header).RetryAfter,
				}
			}
			return nil, err
		}

		var user User
		if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
			return nil, err
		}
		return &user, nil
	}

	var user User
	// Best Practice: Use DoWithContext for context propagation in go-gh
	err := c.rest.DoWithContext(ctx, http.MethodGet, "user", nil, &user)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch current user: %w", err)
	}
	return &user, nil
}

// MarkThreadAsRead marks a single notification thread as read.
func (c *ghClient) MarkThreadAsRead(ctx context.Context, threadID string) error {
	if os.Getenv("GH_ORBIT_SKIP_AUTH") == "1" {
		path := fmt.Sprintf("%snotifications/threads/%s", c.baseURL, threadID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPatch, path, nil)
		if err != nil {
			return err
		}
		resp, err := c.http.Do(req) // #nosec G704: Trusted E2E mock URL
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		c.ReportRateLimit(ParseRateLimitInfo(resp.Header))

		if err := MapHTTPError(resp.StatusCode); err != nil {
			if errors.Is(err, types.ErrRateLimited) {
				return &models.RateLimitError{
					Resource:   "core",
					RetryAfter: ParseRateLimitInfo(resp.Header).RetryAfter,
				}
			}
			return err
		}
		return nil
	}

	path := fmt.Sprintf("notifications/threads/%s", threadID)
	// Best Practice: Use DoWithContext for context propagation in go-gh
	return c.rest.DoWithContext(ctx, http.MethodPatch, path, nil, nil)
}

// REST returns the underlying REST client configured by go-gh.
func (c *ghClient) REST() RESTClient {
	return c.rest
}

// GQL returns the underlying GQL client configured by go-gh.
func (c *ghClient) GQL() GraphQLClient {
	return c.gql
}

// HTTP returns the underlying http.Client configured by go-gh.
func (c *ghClient) HTTP() *http.Client {
	return c.http
}

// BaseURL returns the GitHub API base URL.
func (c *ghClient) BaseURL() string {
	return c.baseURL
}
