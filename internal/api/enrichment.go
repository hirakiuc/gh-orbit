package api

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
)

// EnrichmentResult holds the fetched details for a notification.
type EnrichmentResult struct {
	Body    string
	HTMLURL string
	Author  string
}

// EnrichmentEngine handles fetching and caching of notification details.
type EnrichmentEngine struct {
	client *Client
	db     *db.DB
	logger *slog.Logger
	cache  map[string]EnrichmentResult
	mu     sync.RWMutex
}

func NewEnrichmentEngine(client *Client, database *db.DB, logger *slog.Logger) *EnrichmentEngine {
	return &EnrichmentEngine{
		client: client,
		db:     database,
		logger: logger,
		cache:  make(map[string]EnrichmentResult),
	}
}

// FetchDetail retrieves detailed content for a notification, using cache if available.
func (e *EnrichmentEngine) FetchDetail(u string, subjectType string) (EnrichmentResult, error) {
	e.mu.RLock()
	if res, ok := e.cache[u]; ok {
		e.mu.RUnlock()
		e.logger.Debug("using cached notification detail", "url", u)
		return res, nil
	}
	e.mu.RUnlock()

	e.logger.Debug("fetching notification detail", "url", u, "type", subjectType)

	// Strip base URL if present to use with REST client
	path := strings.TrimPrefix(u, e.client.BaseURL())

	var data struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
			} `json:"author"`
		} `json:"commit"`
	}

	err := e.client.REST().Get(path, &data)
	if err != nil {
		return EnrichmentResult{}, fmt.Errorf("failed to fetch detail from %s: %w", u, err)
	}

	res := EnrichmentResult{
		Body:    data.Body,
		Author:  data.User.Login,
		HTMLURL: data.HTMLURL,
	}

	// Handle specific types
	switch subjectType {
	case "Commit":
		if res.Body == "" {
			res.Body = data.Commit.Message
		}
		if res.Author == "" {
			res.Author = data.Commit.Author.Name
		}
	}

	e.mu.Lock()
	e.cache[u] = res
	e.mu.Unlock()

	return res, nil
}

// GetEnrichmentCmd creates a Bubble Tea command to enrich a notification.
// It wraps the API call and the database persistence.
func (e *EnrichmentEngine) GetEnrichmentCmd(id, u, subjectType string, successMsg func(EnrichmentResult) tea.Msg, errorMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		res, err := e.FetchDetail(u, subjectType)
		if err != nil {
			return errorMsg(err)
		}

		// Persist to DB
		err = e.db.EnrichNotification(id, res.Body, res.Author, res.HTMLURL)
		if err != nil {
			return errorMsg(err)
		}

		return successMsg(res)
	}
}
