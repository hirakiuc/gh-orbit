package engine

import (
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMCPServer_Registration_Coverage(t *testing.T) {
	mockRepo := mocks.NewMockRepository(t)
	mockEnrich := mocks.NewMockEnricher(t)
	mockGH := mocks.NewMockGitHubClient(t)
	mockSync := mocks.NewMockSyncer(t)

	// Ensure mock implements Repository interface required by MCPServer
	mockRepo.EXPECT().ArchiveThread(mock.Anything, mock.Anything).Return(nil).Maybe()
	mockRepo.EXPECT().ListNotifications(mock.Anything).Return(nil, nil).Maybe()

	engine := &CoreEngine{
		DB:     mockRepo,
		Enrich: mockEnrich,
		Client: mockGH,
		Sync:   mockSync,
	}

	s := NewMCPServer(engine, "/tmp/test.sock", true, false)

	t.Run("Initialization coverage", func(t *testing.T) {
		assert.NotNil(t, s.server)
		
		// This exercises registration closures
		s.registerTools()
		s.registerResources()
		
		assert.Greater(t, len(s.server.ListTools()), 0)
	})
}
