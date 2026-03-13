package api

import (
	"context"
	"log/slog"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestEnrichmentEngine_FetchDetail(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	
	mockClient := mocks.NewMockGitHubClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockREST := mocks.NewMockRESTClient(t)
	
	mockClient.EXPECT().BaseURL().Return("https://api.github.com/").Maybe()
	mockClient.EXPECT().REST().Return(mockREST).Maybe()

	// Mock successful REST fetch
	mockREST.EXPECT().DoWithContext(mock.Anything, "GET", "repos/o/r/pulls/1", mock.Anything, mock.Anything).
		Return(nil).Once()
	
	engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, logger)
	t.Cleanup(func() { engine.Shutdown(ctx) })
	
	u := "https://api.github.com/repos/o/r/pulls/1"
	res, err := engine.FetchDetail(ctx, u, "PullRequest")
	
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestEnrichmentEngine_Caching(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	
	mockClient := mocks.NewMockGitHubClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	
	engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, logger)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	u := "https://api.github.com/cache-test"
	res := EnrichmentResult{
		ResourceState: "OPEN",
		FetchedAt:     time.Now(),
	}

	// Manually seed cache
	engine.mu.Lock()
	engine.cache[u] = res
	engine.mu.Unlock()

	// Should hit cache
	got, err := engine.FetchDetail(ctx, u, "Issue")
	require.NoError(t, err)
	assert.Equal(t, "OPEN", got.ResourceState)
}

func TestEnrichmentEngine_HybridBatch(t *testing.T) {
	ctx := context.Background()
	logger := slog.Default()
	
	mockClient := mocks.NewMockGitHubClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockGQL := mocks.NewMockGraphQLClient(t)
	
	mockClient.EXPECT().GQL().Return(mockGQL).Maybe()
	
	// Mock GraphQL response
	mockGQL.EXPECT().DoWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

	engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, logger)
	t.Cleanup(func() { engine.Shutdown(ctx) })

	notifs := []types.NotificationWithState{
		{Notification: types.Notification{GitHubID: "1", SubjectType: "Issue", SubjectURL: "url1", SubjectNodeID: "node1"}},
	}

	results := engine.FetchHybridBatch(ctx, notifs)
	require.NotNil(t, results)
}

func TestEnrichmentEngine_Pruning(t *testing.T) {
	ctx := context.Background()
	engine := NewEnrichmentEngine(ctx, nil, nil, slog.Default())
	t.Cleanup(func() { engine.Shutdown(ctx) })

	engine.mu.Lock()
	engine.cache["old"] = EnrichmentResult{FetchedAt: time.Now().Add(-20 * time.Minute)}
	engine.cache["new"] = EnrichmentResult{FetchedAt: time.Now()}
	engine.mu.Unlock()

	engine.pruneExpired(ctx)

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	assert.NotContains(t, engine.cache, "old")
	assert.Contains(t, engine.cache, "new")
}

func TestEnrichmentEngine_Cmd(t *testing.T) {
	ctx := context.Background()
	mockClient := mocks.NewMockGitHubClient(t)
	mockClient.EXPECT().ReportRateLimit(mock.Anything).Return().Maybe()
	mockRepo := mocks.NewMockEnrichmentRepository(t)
	mockREST := mocks.NewMockRESTClient(t)
	
	mockClient.EXPECT().BaseURL().Return("https://api.github.com/").Maybe()
	mockClient.EXPECT().REST().Return(mockREST).Maybe()
	mockREST.EXPECT().DoWithContext(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	
	// Expect DB enrichment call
	mockRepo.EXPECT().EnrichNotification(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	engine := NewEnrichmentEngine(ctx, mockClient, mockRepo, slog.Default())
	t.Cleanup(func() { engine.Shutdown(ctx) })

	successCalled := false
	cmd := engine.GetEnrichmentCmd("id", "https://api.github.com/u", "Issue", 
		func(res EnrichmentResult) tea.Msg { successCalled = true; return nil },
		func(err error) tea.Msg { return nil })
	
	require.NotNil(t, cmd)
	_ = cmd()
	assert.True(t, successCalled)
}
