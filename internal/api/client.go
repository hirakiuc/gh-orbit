package api

import (
	"fmt"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

// Client wraps the GitHub REST API client.
type Client struct {
	rest *api.RESTClient
	host string
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

	return &Client{
		rest: rest,
		host: host,
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

// REST returns the underlying go-gh REST client.
func (c *Client) REST() *api.RESTClient {
	return c.rest
}
