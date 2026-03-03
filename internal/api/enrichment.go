package api

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	logger *slog.Logger
	cache  map[string]EnrichmentResult
	mu     sync.RWMutex
}

func NewEnrichmentEngine(client *Client, logger *slog.Logger) *EnrichmentEngine {
	return &EnrichmentEngine{
		client: client,
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
