package github

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
)

// User represents the GitHub API response for a user.
type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// LogValue implements slog.LogValuer to redact sensitive user data.
func (u User) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("id", u.ID),
		slog.String("login", "<REDACTED>"),
	)
}

// Notification represents the GitHub API response for a notification.
type Notification struct {
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

// Client defines the operations required from the GitHub API client.
type Client interface {
	CurrentUser(ctx context.Context) (*User, error)
	MarkThreadAsRead(ctx context.Context, threadID string) error
	REST() RESTClient
	GQL() GraphQLClient
	HTTP() *http.Client
	BaseURL() string
	SetRateLimitUpdates(ch chan models.RateLimitInfo)
	ReportRateLimit(info models.RateLimitInfo)
}

// RESTClient defines the minimum interface needed from go-gh REST client.
type RESTClient interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response any) error
}

// GraphQLClient defines the minimum interface needed from go-gh GQL client.
type GraphQLClient interface {
	DoWithContext(ctx context.Context, query string, variables map[string]any, response any) error
}

// Fetcher defines the interface for retrieving notifications from an external source.
type Fetcher interface {
	FetchNotifications(ctx context.Context, meta *models.SyncMeta, force bool) ([]Notification, *models.SyncMeta, models.RateLimitInfo, error)
}
