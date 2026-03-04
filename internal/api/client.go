package api

import (
	"fmt"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

// Client wraps the GitHub REST and GQL API clients.
type Client struct {
	rest    *api.RESTClient
	gql     *api.GraphQLClient
	http    *http.Client
	host    string
	baseURL string
}

// NewClient initializes a new GitHub API client using go-gh.
func NewClient() (*Client, error) {
	host, _ := auth.DefaultHost()

	opts := api.ClientOptions{
		Host:        host,
		EnableCache: true, // Enable ETag support for quota preservation
	}

	rest, err := api.NewRESTClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
	}

	gql, err := api.NewGraphQLClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create GQL client: %w", err)
	}

	httpClient, err := api.NewHTTPClient(opts)
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
func (c *Client) REST() *api.RESTClient {
	return c.rest
}

// GQL returns the underlying GQL client configured by go-gh.
func (c *Client) GQL() *api.GraphQLClient {
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
