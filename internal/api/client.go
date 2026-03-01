package api

import (
	"fmt"
	"net/http"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

// Client wraps the GitHub REST API client.
type Client struct {
	rest    *api.RESTClient
	http    *http.Client
	host    string
	baseURL string
}

// NewClient initializes a new GitHub API client using go-gh.
func NewClient() (*Client, error) {
	host, _ := auth.DefaultHost()

	opts := api.ClientOptions{
		Host: host,
	}

	rest, err := api.NewRESTClient(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create REST client: %w", err)
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

// HTTP returns the underlying http.Client configured by go-gh.
func (c *Client) HTTP() *http.Client {
	return c.http
}

// BaseURL returns the GitHub API base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}
