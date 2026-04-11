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

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
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

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		res, err := engine.FetchDetail(ctx, "url", "Issue", false)
		assert.NoError(t, err)
		assert.Equal(t, "Issue Body", res.Body)
		assert.Equal(t, "Closed", res.ResourceState)
		assert.Equal(t, "COMPLETED", res.ResourceSubState)
	})

	t.Run("Cache Hit", func(t *testing.T) {
		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
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

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
		t.Cleanup(func() { engine.Shutdown(ctx) })

		cached := models.EnrichmentResult{Body: "cached", FetchedAt: time.Now()}
		engine.cache["url"] = cached

		res, err := engine.FetchDetail(ctx, "url", "Issue", true)
		assert.NoError(t, err)
		assert.Equal(t, "fresh", res.Body)
	})
}

func TestEnrichmentEngine_FetchHybridBatch(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockGitHubClient(t)
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

		engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
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
