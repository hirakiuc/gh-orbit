package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestEnrichmentEngine_FetchDetail(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockGitHubClient(t)
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
						Body             string `json:"body"`
						HTMLURL          string `json:"url"`
						Author           struct {
							Login string `json:"login"`
						} `json:"author"`
						State          string `json:"state"`
						Merged         bool   `json:"merged"`
						IsDraft        bool   `json:"isDraft"`
						ResourceSubState string `json:"reviewDecision"`
					} `json:"pullRequest"`
				} `json:"repository"`
			})
			res.Repository.PullRequest.Body = "PR Body"
			res.Repository.PullRequest.ResourceSubState = "APPROVED"
		}).Return(nil).Once()

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		res, err := engine.FetchDetail(ctx, "https://api.github.com/repos/o/r/pulls/1", "PullRequest")
		assert.NoError(t, err)
		assert.Equal(t, "PR Body", res.Body)
		assert.Equal(t, "APPROVED", res.ResourceSubState)
	})

	t.Run("Successful Fetch (Issue)", func(t *testing.T) {
		mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "url", nil, mock.Anything).Return(nil).Once()

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		res, err := engine.FetchDetail(ctx, "url", "Issue")
		assert.NoError(t, err)
		assert.NotEmpty(t, res.FetchedAt)
	})

	t.Run("Cache Hit", func(t *testing.T) {
		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		cached := models.EnrichmentResult{Body: "cached", FetchedAt: time.Now()}
		engine.cache["url"] = cached

		res, err := engine.FetchDetail(ctx, "url", "Issue")
		assert.NoError(t, err)
		assert.Equal(t, "cached", res.Body)
	})
}

func TestEnrichmentEngine_FetchHybridBatch(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockGitHubClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockGQL := mocks.NewMockGraphQLClient(t)

	mockClient.EXPECT().GQL().Return(mockGQL).Maybe()

	t.Run("Batch Fetch Nodes", func(t *testing.T) {
		mockGQL.EXPECT().DoWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		notifs := []triage.NotificationWithState{
			{Notification: triage.Notification{GitHubID: "1", SubjectNodeID: "node1"}},
		}

		results := engine.FetchHybridBatch(ctx, notifs)
		assert.NotNil(t, results)
	})
}

func TestEnrichmentEngine_Pruning(t *testing.T) {
	ctx := context.Background()
	engine := NewEnrichmentEngine(ctx, nil, nil, slog.Default())

	engine.mu.Lock()
	engine.cache["old"] = models.EnrichmentResult{FetchedAt: time.Now().Add(-20 * time.Minute)}
	engine.cache["new"] = models.EnrichmentResult{FetchedAt: time.Now()}
	engine.mu.Unlock()

	engine.pruneExpired(ctx)

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotContains(t, engine.cache, "old")
	assert.Contains(t, engine.cache, "new")
}
