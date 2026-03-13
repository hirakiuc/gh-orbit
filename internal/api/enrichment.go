package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
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

// EnrichmentEngine handles fetching and caching of notification details.
type EnrichmentEngine struct {
	client github.Client
	db     types.EnrichmentRepository
	logger *slog.Logger
	cache  map[string]models.EnrichmentResult
	mu     sync.RWMutex
	sf     singleflight.Group
	done   chan struct{}
}

func NewEnrichmentEngine(ctx context.Context, client github.Client, database types.EnrichmentRepository, logger *slog.Logger) *EnrichmentEngine {
	e := &EnrichmentEngine{
		client: client,
		db:     database,
		logger: logger,
		cache:  make(map[string]models.EnrichmentResult),
		done:   make(chan struct{}),
	}
	
	// Start background pruning worker with lifecycle-managed context
	// #nosec G118: Supervisor context used for background worker longevity
	go e.pruningWorker(ctx)
	
	return e
}

func (e *EnrichmentEngine) Shutdown(ctx context.Context) {
	close(e.done)
	e.logger.DebugContext(ctx, "enrichment engine shutdown complete")
}

// FetchDetail retrieves detailed information for a notification from GitHub.
func (e *EnrichmentEngine) FetchDetail(ctx context.Context, u string, subjectType string) (models.EnrichmentResult, error) {
	// 1. Check local cache
	e.mu.RLock()
	if res, ok := e.cache[u]; ok {
		if time.Since(res.FetchedAt) < ContentTTL {
			e.mu.RUnlock()
			return res, nil
		}
	}
	e.mu.RUnlock()

	// 2. Execute Fetch with singleflight to avoid redundant API calls
	val, err, _ := e.sf.Do(u, func() (any, error) {
		tracer := config.GetTracer()
		ctx, span := tracer.Start(ctx, "enrichment.fetch_detail",
			trace.WithAttributes(
				attribute.String("url", u),
				attribute.String("type", subjectType),
			),
		)
		defer span.End()

		var res models.EnrichmentResult
		var err error

		switch subjectType {
		case "PullRequest", "Issue":
			res, err = e.fetchREST(ctx, u)
		default:
			// Releases and others don't have descriptions in REST API without extra calls
			res = models.EnrichmentResult{HTMLURL: u, FetchedAt: time.Now()}
		}

		if err != nil {
			return nil, err
		}

		// 3. Update cache
		e.mu.Lock()
		e.cache[u] = res
		e.mu.Unlock()

		return res, nil
	})

	if err != nil {
		return models.EnrichmentResult{}, err
	}

	return val.(models.EnrichmentResult), nil
}

func (e *EnrichmentEngine) fetchREST(ctx context.Context, u string) (models.EnrichmentResult, error) {
	var data struct {
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
	}

	// Use internal REST client for authenticated requests
	// go-gh handles the relative path vs absolute URL automatically.
	err := e.client.REST().DoWithContext(ctx, "GET", u, nil, &data)
	if err != nil {
		return models.EnrichmentResult{}, fmt.Errorf("REST fetch failed: %w", err)
	}

	resourceState := ""
	if data.State != "" {
		resourceState = strings.ToUpper(data.State[:1]) + strings.ToLower(data.State[1:])
	}

	return models.EnrichmentResult{
		Body:          data.Body,
		HTMLURL:       data.HTMLURL,
		Author:        data.User.Login,
		ResourceState: resourceState,
		FetchedAt:     time.Now(),
	}, nil
}

// FetchHybridBatch retrieves metadata for multiple items using GQL for efficiency.
func (e *EnrichmentEngine) FetchHybridBatch(ctx context.Context, notifications []types.NotificationWithState) map[string]models.EnrichmentResult {
	results := make(map[string]models.EnrichmentResult)
	var nodeIDs []string

	for _, n := range notifications {
		if n.SubjectNodeID != "" {
			nodeIDs = append(nodeIDs, n.SubjectNodeID)
		}
	}

	if len(nodeIDs) == 0 {
		return results
	}

	// Fetch states in batches of 50 (GQL limit)
	for i := 0; i < len(nodeIDs); i += 50 {
		end := i + 50
		if end > len(nodeIDs) {
			end = len(nodeIDs)
		}
		e.fetchByNodeIDs(ctx, nodeIDs[i:end], results)
	}

	return results
}

func (e *EnrichmentEngine) fetchByNodeIDs(ctx context.Context, ids []string, results map[string]models.EnrichmentResult) {
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
	variables := map[string]any{"ids": ids}
	
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

	// Best Practice: Use DoWithContext for generic context propagation in go-gh GQL
	// Signature: DoWithContext(ctx, query, variables, response)
	err := e.client.GQL().DoWithContext(ctx, queryString, variables, &data)
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

	e.client.ReportRateLimit(types.RateLimitInfo{
		Limit:     data.RateLimit.Cost + data.RateLimit.Remaining, // Best guess for Limit
		Remaining: data.RateLimit.Remaining,
		Used:      data.RateLimit.Cost,
	})

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
			if err := e.db.UpdateResourceStateByNodeID(ctx, node.ID, state); err != nil {
				e.logger.ErrorContext(ctx, "enrichment: failed to update resource state", "node_id", node.ID, "error", err)
			}
			
			// Populate results for immediate TUI refresh
			results[node.ID] = models.EnrichmentResult{
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
			e.logger.DebugContext(ctx, "enrichment: pruning worker stopping (context canceled)")
			return
		case <-e.done:
			e.logger.DebugContext(context.Background(), "enrichment: pruning worker stopping (explicit shutdown)")
			return
		case <-ticker.C:
			e.pruneExpired(ctx)
		}
	}
}

func (e *EnrichmentEngine) pruneExpired(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	count := 0
	for u, res := range e.cache {
		if time.Since(res.FetchedAt) > ContentTTL {
			delete(e.cache, u)
			count++
		}
	}

	if count > 0 && e.logger.Enabled(ctx, slog.LevelDebug) {
		e.logger.DebugContext(ctx, "enrichment: pruned expired cache entries", "count", count)
	}
}
