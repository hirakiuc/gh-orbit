package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"golang.org/x/sync/singleflight"
)

// EnrichmentResult holds the fetched details for a notification.
type EnrichmentResult struct {
	Body          string
	HTMLURL       string
	Author        string
	ResourceState string
}

// EnrichmentEngine handles fetching and caching of notification details.
type EnrichmentEngine struct {
	client *Client
	db     *db.DB
	logger *slog.Logger
	cache  map[string]EnrichmentResult
	mu     sync.RWMutex
	sf     singleflight.Group
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
	// Use singleflight to merge simultaneous requests for the same URL
	res, err, _ := e.sf.Do(u, func() (interface{}, error) {
		return e.fetchDetailRaw(u, subjectType)
	})
	if err != nil {
		return EnrichmentResult{}, err
	}
	return res.(EnrichmentResult), nil
}

func (e *EnrichmentEngine) fetchDetailRaw(u string, subjectType string) (EnrichmentResult, error) {
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
		State   string `json:"state"`
		Merged  bool   `json:"merged"`
		Draft   bool   `json:"draft"`
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

	// Calculate Resource State
	if data.State != "" {
		if data.Merged {
			res.ResourceState = "Merged"
		} else if data.Draft {
			res.ResourceState = "Draft"
		} else {
			if len(data.State) > 0 {
				res.ResourceState = strings.ToUpper(data.State[:1]) + strings.ToLower(data.State[1:])
			}
		}
	}

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

// FetchHybridBatch resolves statuses for multiple items using GraphQL for efficiency.
func (e *EnrichmentEngine) FetchHybridBatch(ctx context.Context, notifications []db.NotificationWithState) map[string]EnrichmentResult {
	if len(notifications) == 0 {
		return nil
	}

	results := make(map[string]EnrichmentResult)
	
	var knownIDs []string
	var discoveryURLs []string
	
	for _, n := range notifications {
		if n.SubjectNodeID != "" {
			knownIDs = append(knownIDs, n.SubjectNodeID)
		} else {
			discoveryURLs = append(discoveryURLs, n.SubjectURL)
		}
	}

	if len(knownIDs) > 0 {
		e.fetchByNodeIDs(ctx, knownIDs, results)
	}

	for _, u := range discoveryURLs {
		select {
		case <-ctx.Done():
			return results
		default:
			for _, n := range notifications {
				if n.SubjectURL == u {
					res, err := e.FetchDetail(u, n.SubjectType)
					if err == nil {
						results[n.GitHubID] = res
						_ = e.db.EnrichNotification(n.GitHubID, res.Body, res.Author, res.HTMLURL, res.ResourceState)
					}
					break
				}
			}
		}
	}
	
	return results
}

func (e *EnrichmentEngine) fetchByNodeIDs(ctx context.Context, ids []string, results map[string]EnrichmentResult) {
	// go-gh/v2 GQL Query uses a struct for the query or a string for the query
	// Let's use a string for the query as it's easier for nodes().
	
	queryString := `
		query($ids: [ID!]!) {
			nodes(ids: $ids) {
				... on PullRequest { id, state, merged, isDraft }
				... on Issue { id, state }
			}
			rateLimit { cost, remaining }
		}
	`
	variables := map[string]interface{}{"ids": ids}
	
	var data struct {
		Nodes []struct {
			ID      string `json:"id"`
			State   string `json:"state"`
			Merged  bool   `json:"merged"`
			IsDraft bool   `json:"isDraft"`
		} `json:"nodes"`
		RateLimit struct {
			Cost      int `json:"cost"`
			Remaining int `json:"remaining"`
		} `json:"rateLimit"`
	}

	// Correct go-gh/v2 GQL signature:
	// func (c *GQLClient) Do(query string, variables map[string]interface{}, response interface{}) error
	// Wait, I saw 'Query' in go doc. Let me re-verify the signature.
	err := e.client.GQL().Do(queryString, variables, &data)
	if err != nil {
		e.logger.Error("graphql batch fetch failed", "error", err)
		return
	}

	e.logger.Debug("graphql batch fetch complete", "cost", data.RateLimit.Cost, "remaining", data.RateLimit.Remaining)

	for _, node := range data.Nodes {
		if node.ID == "" { continue }
		
		state := ""
		if node.Merged {
			state = "Merged"
		} else if node.IsDraft {
			state = "Draft"
		} else if node.State != "" {
			state = strings.ToUpper(node.State[:1]) + strings.ToLower(node.State[1:])
		}

		if state != "" {
			_ = e.db.UpdateResourceStateByNodeID(node.ID, state)
		}
	}
}

// GetEnrichmentCmd creates a Bubble Tea command to enrich a notification.
func (e *EnrichmentEngine) GetEnrichmentCmd(id, u, subjectType string, successMsg func(EnrichmentResult) tea.Msg, errorMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		res, err := e.FetchDetail(u, subjectType)
		if err != nil {
			return errorMsg(err)
		}

		err = e.db.EnrichNotification(id, res.Body, res.Author, res.HTMLURL, res.ResourceState)
		if err != nil {
			return errorMsg(err)
		}

		return successMsg(res)
	}
}
