package api

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestEnrichmentEngine_FetchDetail(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockREST := mocks.NewMockRESTClient(t)

	mockClient.EXPECT().BaseURL().Return("https://api.github.com/").Maybe()
	mockClient.EXPECT().REST().Return(mockREST).Maybe()
	mockGQL := mocks.NewMockGraphQLClient(t)
	mockClient.EXPECT().GQL().Return(mockGQL).Maybe()

	t.Run("Successful Fetch (PullRequest) via GQL", func(t *testing.T) {
		mockGQL.EXPECT().DoWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(ctx context.Context, query string, variables map[string]any, response interface{}) {
			// Simulate GQL response
			res := response.(*struct {
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
			})
			res.Repository.PullRequest.ID = "node_1"
			res.Repository.PullRequest.Body = "PR Body"
			res.Repository.PullRequest.ReviewDecision = "APPROVED"
		}).Return(nil).Once()

		engine, err := NewEnrichmentEngine(ctx, EnrichParams{
			Client: mockClient,
			Config: config.DefaultConfig(),
			DB:     mockRepo,
			Logger: slog.Default(),
		})
		assert.NoError(t, err)
		t.Cleanup(func() { engine.Shutdown(ctx) })

		res, err := engine.FetchDetail(ctx, "https://api.github.com/repos/o/r/pulls/1", "PullRequest", false)
		assert.NoError(t, err)
		assert.Equal(t, "node_1", res.SubjectNodeID)
		assert.Equal(t, "APPROVED", res.ResourceSubState)
	})

	t.Run("Successful Fetch (Issue) with StateReason", func(t *testing.T) {
		mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "url", nil, mock.Anything).Call.Run(func(args mock.Arguments) {
			response := args.Get(4)
			res := response.(*struct {
				ID      string `json:"node_id"`
				Body    string `json:"body"`
				HTMLURL string `json:"html_url"`
				User    struct {
					Login string `json:"login"`
				} `json:"user"`
				State       string  `json:"state"`
				StateReason *string `json:"state_reason"`
			})
			res.ID = "node_issue_1"
			res.Body = "Issue Body"
			res.State = "closed"
			reason := "completed"
			res.StateReason = &reason
		}).Return(nil).Once()

		engine, err := NewEnrichmentEngine(ctx, EnrichParams{
			Client: mockClient,
			Config: config.DefaultConfig(),
			DB:     mockRepo,
			Logger: slog.Default(),
		})
		assert.NoError(t, err)
		t.Cleanup(func() { engine.Shutdown(ctx) })

		res, err := engine.FetchDetail(ctx, "url", "Issue", false)
		assert.NoError(t, err)
		assert.Equal(t, "Issue Body", res.Body)
		assert.Equal(t, "Closed", res.ResourceState)
		assert.Equal(t, "COMPLETED", res.ResourceSubState)
	})

	t.Run("Cache Hit", func(t *testing.T) {
		engine, err := NewEnrichmentEngine(ctx, EnrichParams{
			Client: mockClient,
			Config: config.DefaultConfig(),
			DB:     mockRepo,
			Logger: slog.Default(),
		})
		assert.NoError(t, err)
		t.Cleanup(func() { engine.Shutdown(ctx) })

		cached := models.EnrichmentResult{Body: "cached", FetchedAt: time.Now()}
		engine.cache["url"] = cached

		res, err := engine.FetchDetail(ctx, "url", "Issue", false)
		assert.NoError(t, err)
		assert.Equal(t, "cached", res.Body)
	})

	t.Run("Force Refresh Bypasses Cache", func(t *testing.T) {
		mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "url", nil, mock.Anything).Call.Run(func(args mock.Arguments) {
			response := args.Get(4)
			res := response.(*struct {
				ID      string `json:"node_id"`
				Body    string `json:"body"`
				HTMLURL string `json:"html_url"`
				User    struct {
					Login string `json:"login"`
				} `json:"user"`
				State       string  `json:"state"`
				StateReason *string `json:"state_reason"`
			})
			res.Body = "fresh"
		}).Return(nil).Once()

		engine, err := NewEnrichmentEngine(ctx, EnrichParams{
			Client: mockClient,
			Config: config.DefaultConfig(),
			DB:     mockRepo,
			Logger: slog.Default(),
		})
		assert.NoError(t, err)
		t.Cleanup(func() { engine.Shutdown(ctx) })

		cached := models.EnrichmentResult{Body: "cached", FetchedAt: time.Now()}
		engine.cache["url"] = cached

		res, err := engine.FetchDetail(ctx, "url", "Issue", true)
		assert.NoError(t, err)
		assert.Equal(t, "fresh", res.Body)
	})

	t.Run("Chaos Paths - API Errors", func(t *testing.T) {
		tests := []struct {
			name     string
			apiErr   error
			expected error
		}{
			{"Unauthorized", types.ErrUnauthorized, types.ErrUnauthorized},
			{"Internal Error", types.ErrInternalServerError, types.ErrInternalServerError},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "url", nil, mock.Anything).Return(tt.apiErr).Once()
				engine, _ := NewEnrichmentEngine(ctx, EnrichParams{Client: mockClient, Config: config.DefaultConfig(), DB: mockRepo, Logger: slog.Default()})
				_, err := engine.FetchDetail(ctx, "url", "Issue", false)
				assert.ErrorIs(t, err, tt.expected)
			})
		}
	})
}

func TestEnrichmentEngine_FetchHybridBatch(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockGQL := mocks.NewMockGraphQLClient(t)

	mockClient.EXPECT().GQL().Return(mockGQL).Maybe()

	t.Run("Batch Fetch Nodes - Typed Mapping", func(t *testing.T) {
		mockGQL.EXPECT().DoWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(ctx context.Context, query string, variables map[string]any, response interface{}) {
			res := response.(*struct {
				Nodes []struct {
					Typename       string  `json:"__typename"`
					ID             string  `json:"id"`
					State          string  `json:"state"`
					Merged         bool    `json:"merged"`
					IsDraft        bool    `json:"isDraft"`
					ReviewDecision string  `json:"reviewDecision"`
					StateReason    *string `json:"stateReason"`
					Closed         bool    `json:"closed"`
				} `json:"nodes"`
				RateLimit struct {
					Cost      int `json:"cost"`
					Remaining int `json:"remaining"`
				} `json:"rateLimit"`
			})
			reason := "COMPLETED"
			res.Nodes = []struct {
				Typename       string  `json:"__typename"`
				ID             string  `json:"id"`
				State          string  `json:"state"`
				Merged         bool    `json:"merged"`
				IsDraft        bool    `json:"isDraft"`
				ReviewDecision string  `json:"reviewDecision"`
				StateReason    *string `json:"stateReason"`
				Closed         bool    `json:"closed"`
			}{
				{Typename: "PullRequest", ID: "pr1", State: "MERGED", Merged: true, ReviewDecision: "APPROVED"},
				{Typename: "Issue", ID: "issue1", State: "CLOSED", StateReason: &reason},
				{Typename: "Discussion", ID: "disc1", Closed: true, StateReason: &reason},
			}
		}).Return(nil).Once()

		mockRepo.EXPECT().UpdateResourceStateByNodeID(mock.Anything, "pr1", "Merged", "APPROVED").Return(nil).Once()
		mockRepo.EXPECT().UpdateResourceStateByNodeID(mock.Anything, "issue1", "Closed", "COMPLETED").Return(nil).Once()
		mockRepo.EXPECT().UpdateResourceStateByNodeID(mock.Anything, "disc1", "Closed", "COMPLETED").Return(nil).Once()

		engine, err := NewEnrichmentEngine(ctx, EnrichParams{
			Client: mockClient,
			Config: config.DefaultConfig(),
			DB:     mockRepo,
			Logger: slog.Default(),
		})
		assert.NoError(t, err)
		t.Cleanup(func() { engine.Shutdown(ctx) })

		notifs := []triage.NotificationWithState{
			{Notification: triage.Notification{GitHubID: "1", SubjectNodeID: "pr1"}},
			{Notification: triage.Notification{GitHubID: "2", SubjectNodeID: "issue1"}},
			{Notification: triage.Notification{GitHubID: "3", SubjectNodeID: "disc1"}},
		}

		results := engine.FetchHybridBatch(ctx, notifs, false)
		assert.Len(t, results, 3)
		assert.Equal(t, "Merged", results["pr1"].ResourceState)
		assert.Equal(t, "APPROVED", results["pr1"].ResourceSubState)
		assert.Equal(t, "Closed", results["issue1"].ResourceState)
		assert.Equal(t, "COMPLETED", results["issue1"].ResourceSubState)
	})
}

func TestEnrichmentEngine_Pruning(t *testing.T) {
	ctx := context.Background()
	engine, _ := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mocks.NewMockClient(t),
		Config: config.DefaultConfig(),
		DB:     mocks.NewMockEnrichmentRepository(t),
		Logger: slog.Default(),
	})
	t.Cleanup(func() { engine.Shutdown(ctx) })
	engine.config = &config.Config{
		Enrichment: config.EnrichmentConfig{
			ContentTTLSeconds: 60,
		},
	}

	engine.mu.Lock()
	engine.cache["old"] = models.EnrichmentResult{FetchedAt: time.Now().Add(-2 * time.Minute)}
	engine.cache["new"] = models.EnrichmentResult{FetchedAt: time.Now().Add(-30 * time.Second)}
	engine.mu.Unlock()

	engine.pruneExpired(ctx)

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotContains(t, engine.cache, "old")
	assert.Contains(t, engine.cache, "new")
}

func TestEnrichmentEngine_ContentTTL(t *testing.T) {
	t.Run("NilConfigFallsBackToDefault", func(t *testing.T) {
		engine := &EnrichmentEngine{}
		assert.Equal(t, ContentTTL, engine.contentTTL())
	})

	t.Run("ConfiguredZeroTTLIsPreserved", func(t *testing.T) {
		engine := &EnrichmentEngine{
			config: &config.Config{
				Enrichment: config.EnrichmentConfig{
					ContentTTLSeconds: 0,
				},
			},
		}
		assert.Zero(t, engine.contentTTL())
	})

	t.Run("ConfiguredTTLIsUsed", func(t *testing.T) {
		engine := &EnrichmentEngine{
			config: &config.Config{
				Enrichment: config.EnrichmentConfig{
					ContentTTLSeconds: 45,
				},
			},
		}
		assert.Equal(t, 45*time.Second, engine.contentTTL())
	})
}

func TestEnrichmentEngine_PersistFetchedDetailPreservesCache(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)

	engine, err := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mockClient,
		Config: config.DefaultConfig(),
		DB:     mockRepo,
		Logger: slog.Default(),
	})
	assert.NoError(t, err)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	res := models.EnrichmentResult{
		SubjectNodeID: "node-1",
		Body:          "fresh body",
		Author:        "octocat",
		HTMLURL:       "https://github.com/o/r/pull/1",
		FetchedAt:     time.Now(),
	}

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-1", "node-1", "fresh body", "octocat", "https://github.com/o/r/pull/1", "", "").
		Return(nil).
		Once()

	err = engine.PersistFetchedDetail(ctx, "notif-1", "https://api.github.com/repos/o/r/pulls/1", res)
	assert.NoError(t, err)

	got, err := engine.FetchDetail(ctx, "https://api.github.com/repos/o/r/pulls/1", "PullRequest", false)
	assert.NoError(t, err)
	assert.Equal(t, "fresh body", got.Body)
	assert.Equal(t, "node-1", got.SubjectNodeID)
}

func TestEnrichmentEngine_PersistIndependentDetailInvalidatesCacheByNodeID(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockREST := mocks.NewMockRESTClient(t)

	mockClient.EXPECT().REST().Return(mockREST).Maybe()

	engine, err := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mockClient,
		Config: config.DefaultConfig(),
		DB:     mockRepo,
		Logger: slog.Default(),
	})
	assert.NoError(t, err)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	cached := models.EnrichmentResult{
		SubjectNodeID: "node-2",
		Body:          "cached body",
		Author:        "octocat",
		HTMLURL:       "https://github.com/o/r/issues/2",
		FetchedAt:     time.Now(),
	}

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-2", "node-2", "cached body", "octocat", "https://github.com/o/r/issues/2", "", "").
		Return(nil).
		Once()
	assert.NoError(t, engine.PersistFetchedDetail(ctx, "notif-2", "https://api.github.com/repos/o/r/issues/2", cached))

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-2", "node-2", "persisted body", "hirakiuc", "https://github.com/o/r/issues/2", "Closed", "COMPLETED").
		Return(nil).
		Once()
	assert.NoError(t, engine.PersistIndependentDetail(ctx, "notif-2", "node-2", "persisted body", "hirakiuc", "https://github.com/o/r/issues/2", "Closed", "COMPLETED"))

	mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "https://api.github.com/repos/o/r/issues/2", nil, mock.Anything).Run(func(ctx context.Context, method string, path string, body io.Reader, response any) {
		res := response.(*struct {
			ID      string `json:"node_id"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			State       string  `json:"state"`
			StateReason *string `json:"state_reason"`
		})
		res.ID = "node-2"
		res.Body = "refetched body"
		res.HTMLURL = "https://github.com/o/r/issues/2"
		res.User.Login = "hirakiuc"
	}).Return(nil).Once()

	got, err := engine.FetchDetail(ctx, "https://api.github.com/repos/o/r/issues/2", "Issue", false)
	assert.NoError(t, err)
	assert.Equal(t, "refetched body", got.Body)
}

func TestEnrichmentEngine_PersistIndependentDetailInvalidatesCacheByHTMLURL(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockREST := mocks.NewMockRESTClient(t)

	mockClient.EXPECT().REST().Return(mockREST).Maybe()

	engine, err := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mockClient,
		Config: config.DefaultConfig(),
		DB:     mockRepo,
		Logger: slog.Default(),
	})
	assert.NoError(t, err)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	cached := models.EnrichmentResult{
		SubjectNodeID: "node-4",
		Body:          "cached body",
		Author:        "octocat",
		HTMLURL:       "https://github.com/o/r/issues/4",
		FetchedAt:     time.Now(),
	}

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-4", "node-4", "cached body", "octocat", "https://github.com/o/r/issues/4", "", "").
		Return(nil).
		Once()
	assert.NoError(t, engine.PersistFetchedDetail(ctx, "notif-4", "https://api.github.com/repos/o/r/issues/4", cached))

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-4", "", "persisted body", "hirakiuc", "https://github.com/o/r/issues/4", "Closed", "COMPLETED").
		Return(nil).
		Once()
	assert.NoError(t, engine.PersistIndependentDetail(ctx, "notif-4", "", "persisted body", "hirakiuc", "https://github.com/o/r/issues/4", "Closed", "COMPLETED"))

	mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "https://api.github.com/repos/o/r/issues/4", nil, mock.Anything).Run(func(ctx context.Context, method string, path string, body io.Reader, response any) {
		res := response.(*struct {
			ID      string `json:"node_id"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			State       string  `json:"state"`
			StateReason *string `json:"state_reason"`
		})
		res.ID = "node-4"
		res.Body = "refetched from html-url invalidation"
		res.HTMLURL = "https://github.com/o/r/issues/4"
		res.User.Login = "hirakiuc"
	}).Return(nil).Once()

	got, err := engine.FetchDetail(ctx, "https://api.github.com/repos/o/r/issues/4", "Issue", false)
	assert.NoError(t, err)
	assert.Equal(t, "refetched from html-url invalidation", got.Body)
}

func TestEnrichmentEngine_UpdateNodeStateInvalidatesCacheByNodeID(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)

	engine, err := NewEnrichmentEngine(ctx, EnrichParams{
		Client: mockClient,
		Config: config.DefaultConfig(),
		DB:     mockRepo,
		Logger: slog.Default(),
	})
	assert.NoError(t, err)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	mockRepo.EXPECT().
		EnrichNotification(mock.Anything, "notif-3", "node-3", "cached body", "octocat", "https://github.com/o/r/pull/3", "", "").
		Return(nil).
		Once()
	assert.NoError(t, engine.PersistFetchedDetail(ctx, "notif-3", "https://api.github.com/repos/o/r/pulls/3", models.EnrichmentResult{
		SubjectNodeID: "node-3",
		Body:          "cached body",
		Author:        "octocat",
		HTMLURL:       "https://github.com/o/r/pull/3",
		FetchedAt:     time.Now(),
	}))

	mockRepo.EXPECT().UpdateResourceStateByNodeID(mock.Anything, "node-3", "Merged", "APPROVED").Return(nil).Once()
	engine.updateNodeState(ctx, "node-3", "Merged", "APPROVED", map[string]models.EnrichmentResult{})

	engine.mu.RLock()
	_, ok := engine.cache["https://api.github.com/repos/o/r/pulls/3"]
	engine.mu.RUnlock()
	assert.False(t, ok)
}

func TestNewEnrichmentEngine_Guards(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockClient(t)
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	logger := slog.Default()

	t.Run("Missing Client", func(t *testing.T) {
		_, err := NewEnrichmentEngine(ctx, EnrichParams{DB: mockRepo, Config: config.DefaultConfig(), Logger: logger})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "github client is required")
	})

	t.Run("Missing DB", func(t *testing.T) {
		_, err := NewEnrichmentEngine(ctx, EnrichParams{Client: mockClient, Config: config.DefaultConfig(), Logger: logger})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database is required")
	})

	t.Run("Missing Config", func(t *testing.T) {
		_, err := NewEnrichmentEngine(ctx, EnrichParams{Client: mockClient, DB: mockRepo, Logger: logger})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("Missing Logger", func(t *testing.T) {
		_, err := NewEnrichmentEngine(ctx, EnrichParams{Client: mockClient, DB: mockRepo, Config: config.DefaultConfig()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "logger is required")
	})
}
