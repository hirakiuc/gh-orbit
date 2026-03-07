package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

const (
	// StatusTTL is the duration for which a resource status (Open, Merged) is considered fresh.
	StatusTTL = 2 * time.Minute
	// ContentTTL is the duration for which the body content is considered fresh.
	ContentTTL = 10 * time.Minute
)

// EnrichmentResult holds the fetched details for a notification.
type EnrichmentResult struct {
	Body          string
	HTMLURL       string
	Author        string
	ResourceState string
	FetchedAt     time.Time
}

// EnrichmentEngine handles fetching and caching of notification details.
type EnrichmentEngine struct {
	client *Client
	db     EnrichmentRepository
	logger *slog.Logger
	cache  map[string]EnrichmentResult
	mu     sync.RWMutex
	sf     singleflight.Group
}

func NewEnrichmentEngine(ctx context.Context, client *Client, database EnrichmentRepository, logger *slog.Logger) *EnrichmentEngine {
	e := &EnrichmentEngine{
		client: client,
		db:     database,
		logger: logger,
		cache:  make(map[string]EnrichmentResult),
	}
	
	// Start background pruning worker with application context
	go e.pruningWorker(ctx)
	
	return e
}

// FetchDetail retrieves detailed content for a notification, using cache if available and fresh.
func (e *EnrichmentEngine) FetchDetail(ctx context.Context, u string, subjectType string) (EnrichmentResult, error) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "enrichment.fetch_detail",
		trace.WithAttributes(
			attribute.String("url", u),
			attribute.String("type", subjectType),
		),
	)
	defer span.End()

	// 1. Semantic Cache Validation (Optimistic Read)
	e.mu.RLock()
	res, ok := e.cache[u]
	e.mu.RUnlock()

	if ok {
		age := time.Since(res.FetchedAt)
		
		// Tiered Validation
		if age <= StatusTTL && res.ResourceState != "" {
			if e.logger.Enabled(ctx, slog.LevelDebug) {
				e.logger.DebugContext(ctx, "enrichment: cache hit (valid)", "url", u, "age", age)
			}
			span.SetAttributes(attribute.Bool("cache_hit", true))
			return res, nil
		}

		if age > StatusTTL {
			e.logger.DebugContext(ctx, "enrichment: status expired, forcing refresh", 
				"url", u, 
				"age", fmt.Sprintf("%.0fs", age.Seconds()),
				"threshold", fmt.Sprintf("%.0fs", StatusTTL.Seconds()))
			span.SetAttributes(attribute.String("cache_status", "expired"))
		}
	}

	// 2. Use singleflight to merge simultaneous requests for the same URL
	val, err, shared := e.sf.Do(u, func() (interface{}, error) {
		return e.fetchDetailRaw(ctx, u, subjectType)
	})

	if shared {
		e.logger.DebugContext(ctx, "enrichment: request merged via singleflight", "url", u)
		span.SetAttributes(attribute.Bool("singleflight_merged", true))
	}

	if err != nil {
		return EnrichmentResult{}, err
	}
	return val.(EnrichmentResult), nil
}

func (e *EnrichmentEngine) fetchDetailRaw(ctx context.Context, u string, subjectType string) (EnrichmentResult, error) {
	tracer := config.GetTracer()
	_, span := tracer.Start(ctx, "enrichment.api_fetch")
	defer span.End()

	if e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.DebugContext(ctx, "enrichment: cache miss or invalid, fetching from API", "url", u, "type", subjectType)
	}

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
		Body:      data.Body,
		Author:    data.User.Login,
		HTMLURL:   data.HTMLURL,
		FetchedAt: time.Now(),
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

	// Only cache if we have meaningful data
	if res.ResourceState != "" || res.Body != "" {
		e.mu.Lock()
		e.cache[u] = res
		e.mu.Unlock()
	}

	return res, nil
}

// FetchHybridBatch resolves statuses for multiple items using GraphQL for efficiency.
func (e *EnrichmentEngine) FetchHybridBatch(ctx context.Context, notifications []db.NotificationWithState) map[string]EnrichmentResult {
	if len(notifications) == 0 {
		return nil
	}

	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "enrichment.hybrid_batch",
		trace.WithAttributes(attribute.Int("count", len(notifications))),
	)
	defer span.End()

	if e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.DebugContext(ctx, "enrichment: starting hybrid batch fetch", "count", len(notifications))
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

	// 1. Fetch Known IDs via nodes()
	if len(knownIDs) > 0 {
		e.fetchByNodeIDs(ctx, knownIDs, results)
	}

	// 2. Fetch Discovery URLs via individual REST calls (fallback)
	for _, u := range discoveryURLs {
		select {
		case <-ctx.Done():
			return results
		default:
			for _, n := range notifications {
				if n.SubjectURL == u {
					res, err := e.FetchDetail(ctx, u, n.SubjectType)
					if err == nil {
						results[n.GitHubID] = res
						_ = e.db.EnrichNotification(ctx, n.GitHubID, res.Body, res.Author, res.HTMLURL, res.ResourceState)
					}
					break
				}
			}
		}
	}
	
	return results
}

func (e *EnrichmentEngine) fetchByNodeIDs(ctx context.Context, ids []string, results map[string]EnrichmentResult) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "enrichment.gql_batch")
	defer span.End()

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

	err := e.client.GQL().Do(queryString, variables, &data)
	if err != nil {
		e.logger.ErrorContext(ctx, "enrichment: graphql batch fetch failed", "error", err)
		return
	}

	if e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.DebugContext(ctx, "enrichment: graphql batch fetch complete", 
			"cost", data.RateLimit.Cost, 
			"remaining", data.RateLimit.Remaining,
			"node_count", len(data.Nodes))
	}

	span.SetAttributes(
		attribute.Int("gql.cost", data.RateLimit.Cost),
		attribute.Int("gql.nodes", len(data.Nodes)),
	)

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
			_ = e.db.UpdateResourceStateByNodeID(ctx, node.ID, state)
			
			// Populate results for immediate TUI refresh
			results[node.ID] = EnrichmentResult{
				ResourceState: state,
				FetchedAt:     time.Now(),
			}
		}
	}
}

func (e *EnrichmentEngine) pruningWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.DebugContext(ctx, "enrichment: pruning worker stopping")
			return
		case <-ticker.C:
			e.pruneExpired()
		}
	}
}

func (e *EnrichmentEngine) pruneExpired() {
	e.mu.Lock()
	defer e.mu.Unlock()

	count := 0
	for k, v := range e.cache {
		if time.Since(v.FetchedAt) > ContentTTL {
			delete(e.cache, k)
			count++
		}
	}

	if count > 0 && e.logger.Enabled(context.Background(), slog.LevelDebug) {
		e.logger.DebugContext(context.Background(), "enrichment: pruned expired cache entries", "count", count)
	}
}

// GetEnrichmentCmd creates a Bubble Tea command to enrich a notification.
func (e *EnrichmentEngine) GetEnrichmentCmd(id, u, subjectType string, successMsg func(EnrichmentResult) tea.Msg, errorMsg func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		res, err := e.FetchDetail(ctx, u, subjectType)
		if err != nil {
			return errorMsg(err)
		}

		err = e.db.EnrichNotification(ctx, id, res.Body, res.Author, res.HTMLURL, res.ResourceState)
		if err != nil {
			return errorMsg(err)
		}

		return successMsg(res)
	}
}
