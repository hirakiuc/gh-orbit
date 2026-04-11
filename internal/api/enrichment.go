package api

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
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
	config *config.Config
}

func NewEnrichmentEngine(ctx context.Context, client github.Client, database types.EnrichmentRepository, logger *slog.Logger) *EnrichmentEngine {
	cfg, _ := config.Load()
	e := &EnrichmentEngine{
		client: client,
		db:     database,
		logger: logger,
		cache:  make(map[string]models.EnrichmentResult),
		done:   make(chan struct{}),
		config: cfg,
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
func (e *EnrichmentEngine) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	contentTTL := 10 * time.Minute
	if e.config != nil {
		contentTTL = time.Duration(e.config.Enrichment.ContentTTLSeconds) * time.Second
	}

	// 1. Check local cache (unless forced)
	if !force {
		e.mu.RLock()
		if res, ok := e.cache[u]; ok {
			remaining := contentTTL - time.Since(res.FetchedAt)
			if remaining > 0 {
				e.mu.RUnlock()
				return res, nil
			}
		}
		e.mu.RUnlock()
	}

	// 2. Execute Fetch with singleflight to avoid redundant API calls
	// When forced, we use a unique key for singleflight to ensure we don't share with a cached (non-forced) call
	sfKey := u
	if force {
		sfKey = "force:" + u
	}

	val, err, _ := e.sf.Do(sfKey, func() (any, error) {
		tracer := config.GetTracer()
		ctx, span := tracer.Start(ctx, "enrichment.fetch_detail",
			trace.WithAttributes(
				attribute.String("url", u),
				attribute.String("type", subjectType),
				attribute.Bool("force", force),
			),
		)
		defer span.End()

		// Observability: Check if we had a cache entry even if we are forcing/missing
		e.mu.RLock()
		if res, ok := e.cache[u]; ok {
			span.SetAttributes(
				attribute.Bool("enrichment.cache_hit", !force),
				attribute.Bool("enrichment.cache_available", true),
				attribute.String("enrichment.cached_at", res.FetchedAt.Format(time.RFC3339)),
				attribute.Float64("enrichment.cache_age_sec", time.Since(res.FetchedAt).Seconds()),
			)
		} else {
			span.SetAttributes(attribute.Bool("enrichment.cache_hit", false))
		}
		e.mu.RUnlock()

		var res models.EnrichmentResult
		var err error

		switch triage.SubjectType(subjectType) {
		case triage.SubjectPullRequest:
			res, err = e.fetchPullRequestGQL(ctx, u)
		case triage.SubjectIssue:
			res, err = e.fetchREST(ctx, u)
		case triage.SubjectDiscussion:
			res, err = e.fetchREST(ctx, u) // Discussions also supported via REST for basic details
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

func (e *EnrichmentEngine) fetchPullRequestGQL(ctx context.Context, u string) (models.EnrichmentResult, error) {
	owner, repoName := github.ExtractOwnerRepoFromURL(u)
	numberStr := github.ExtractNumberFromURL(u)

	if owner == "" || repoName == "" || numberStr == "" {
		return e.fetchREST(ctx, u)
	}

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return e.fetchREST(ctx, u)
	}

	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "enrichment.gql_fetch")
	defer span.End()

	// Use repository query to get node ID and details
	queryString := `
                query($owner: String!, $repo: String!, $number: Int!) {
                        repository(owner: $owner, name: $repo) {
                                pullRequest(number: $number) {
                                        id
                                        body
                                        url
                                        author { login }
                                        state
                                        merged
                                        isDraft
                                        reviewDecision
                                }
                        }
                }
        `
	variables := map[string]any{
		"owner":  owner,
		"repo":   repoName,
		"number": number,
	}

	// Structural Alignment: Use a dedicated result struct for FetchDetail
	var data struct {
		Repository struct {
			PullRequest struct {
				ID      string `json:"id"`
				Body    string `json:"body"`
				HTMLURL string `json:"url"`
				Author  struct {
					Login string `json:"login"`
				} `json:"author"`
				State          string `json:"state"`
				Merged         bool   `json:"merged"`
				IsDraft        bool   `json:"isDraft"`
				ReviewDecision string `json:"reviewDecision"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	err = e.client.GQL().DoWithContext(ctx, queryString, variables, &data)
	if err != nil {
		return models.EnrichmentResult{}, fmt.Errorf("GQL fetch failed: %w", err)
	}

	pr := data.Repository.PullRequest
	state := ""
	if pr.Merged {
		state = "Merged"
	} else if pr.IsDraft {
		state = "Draft"
	} else if pr.State != "" {
		state = strings.ToUpper(pr.State[:1]) + strings.ToLower(pr.State[1:])
	}

	return models.EnrichmentResult{
		SubjectNodeID:    pr.ID,
		Body:             pr.Body,
		HTMLURL:          pr.HTMLURL,
		Author:           pr.Author.Login,
		ResourceState:    state,
		ResourceSubState: pr.ReviewDecision,
		FetchedAt:        time.Now(),
	}, nil
}

func (e *EnrichmentEngine) fetchREST(ctx context.Context, u string) (models.EnrichmentResult, error) {
	var data struct {
		ID      string `json:"node_id"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		State       string  `json:"state"`
		StateReason *string `json:"state_reason"`
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

	subState := ""
	if data.StateReason != nil {
		subState = strings.ToUpper(*data.StateReason)
	}

	return models.EnrichmentResult{
		SubjectNodeID:    data.ID,
		Body:             data.Body,
		HTMLURL:          data.HTMLURL,
		Author:           data.User.Login,
		ResourceState:    resourceState,
		ResourceSubState: subState,
		FetchedAt:        time.Now(),
	}, nil
}

// FetchHybridBatch retrieves metadata for multiple items using GQL for efficiency.
func (e *EnrichmentEngine) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	results := make(map[string]models.EnrichmentResult)
	var nodeIDs []string

	statusTTL := 2 * time.Minute
	if e.config != nil {
		statusTTL = time.Duration(e.config.Enrichment.StatusTTLSeconds) * time.Second
	}

	for _, n := range notifications {
		if n.SubjectNodeID != "" {
			if !force && n.IsEnriched && n.EnrichedAt.Valid && time.Since(n.EnrichedAt.Time) < statusTTL {
				e.logger.DebugContext(ctx, "enrichment: skipping item within status TTL",
					"node_id", n.SubjectNodeID,
					"enriched_at", n.EnrichedAt.Time.Format(time.RFC3339),
					"ttl", statusTTL)
				continue
			}
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
				__typename
				... on PullRequest { id, state, merged, isDraft, reviewDecision }
				... on Issue { id, state, stateReason }
				... on Discussion { id, closed, stateReason }
			}
			rateLimit { cost, remaining }
		}
	`
	variables := map[string]any{"ids": ids}

	var data struct {
		Nodes []struct {
			Typename       string  `json:"__typename"`
			ID             string  `json:"id"`
			State          string  `json:"state"`
			Merged         bool    `json:"merged"`
			IsDraft        bool    `json:"isDraft"`
			ReviewDecision string  `json:"reviewDecision"`
			StateReason    *string `json:"stateReason"` // Use pointer to handle nulls
			Closed         bool    `json:"closed"`      // Discussion
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

	e.client.ReportRateLimit(models.RateLimitInfo{
		Limit:     data.RateLimit.Cost + data.RateLimit.Remaining, // Best guess for Limit
		Remaining: data.RateLimit.Remaining,
		Used:      data.RateLimit.Cost,
	})

	span.SetAttributes(
		attribute.Int("gql.cost", data.RateLimit.Cost),
		attribute.Int("gql.nodes", len(data.Nodes)),
	)

	for _, node := range data.Nodes {
		if node.ID == "" {
			continue
		}

		state := ""
		subState := ""

		switch triage.SubjectType(node.Typename) {
		case triage.SubjectPullRequest:
			if node.Merged {
				state = "Merged"
			} else if node.IsDraft {
				state = "Draft"
			} else if node.State != "" {
				state = strings.ToUpper(node.State[:1]) + strings.ToLower(node.State[1:])
			}
			subState = node.ReviewDecision
		case triage.SubjectIssue:
			if node.State != "" {
				state = strings.ToUpper(node.State[:1]) + strings.ToLower(node.State[1:])
			}
			if node.StateReason != nil {
				subState = *node.StateReason
			}
		case triage.SubjectDiscussion:
			if node.Closed {
				state = "Closed"
			} else {
				state = "Open"
			}
			if node.StateReason != nil {
				subState = *node.StateReason
			}
		}

		if state != "" || subState != "" {
			// Observability: Log transition
			e.logger.DebugContext(ctx, "enrichment: node state fetched",
				"node_id", node.ID,
				"new_state", state,
				"new_substate", subState)

			if err := e.db.UpdateResourceStateByNodeID(ctx, node.ID, state, subState); err != nil {
				e.logger.ErrorContext(ctx, "enrichment: failed to update resource state", "node_id", node.ID, "error", err)
			}

			// Populate results for immediate TUI refresh
			results[node.ID] = models.EnrichmentResult{
				ResourceState:    state,
				ResourceSubState: subState,
				FetchedAt:        time.Now(),
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
