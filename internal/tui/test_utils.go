package tui

import (
	"log/slog"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/mock"
)

// TestingT is a common interface for *testing.T and *testing.B
type TestingT interface {
	mock.TestingT
	Cleanup(func())
}

// newTestModel creates a model with basic mocks.
func newTestModel(t TestingT) *Model {
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	userID := "test-user"

	// Mock engines
	mockSyncer := mocks.NewMockSyncer(t)
	mockEnricher := mocks.NewMockEnricher(t)
	mockTraffic := mocks.NewMockTrafficController(t)
	mockTraffic.EXPECT().Remaining().Return(5000).Maybe()
	mockAlerter := mocks.NewMockAlerter(t)
	mockRepo := mocks.NewMockRepository(t)
	mockClient := mocks.NewMockGitHubClient(t)
	mockExecutor := mocks.NewMockCommandExecutor(t)

	// Basic bridge status mock (used in NewModel or Transition)
	mockSyncer.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()
	mockAlerter.EXPECT().BridgeStatus().Return(types.StatusHealthy).Maybe()

	m := NewModel(
		userID,
		cfg,
		logger,
		mockRepo,
		mockClient,
		mockSyncer,
		mockEnricher,
		mockTraffic,
		mockAlerter,
		WithExecutor(mockExecutor),
	)
	
	m.heartbeatInterval = time.Millisecond
	m.clockInterval = time.Millisecond
	m.ui.toastTimeout = time.Millisecond
	m.bridgeStatus = types.StatusHealthy
	
	// Initialize renderer
	m.width = 80
	m.height = 24
	m.updateMarkdownRenderer()
	
	// Provision a default notification for tests
	m.allNotifications = []types.NotificationWithState{
		{
			Notification: types.Notification{
				GitHubID: "default-id",
				SubjectTitle: "Default Title",
				RepositoryFullName: "owner/repo",
				SubjectType: "",
			},
		},
	}
	m.applyFilters()
	
	m.ui.SetSize(80, 24)
	return m
}

// stripANSI is a simple utility to remove ANSI codes for content-based assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inANSI := false
	for _, r := range s {
		if r == '\x1b' {
			inANSI = true
			continue
		}
		if inANSI {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inANSI = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// executeCmd is a helper to recursively execute tea.Cmd and tea.BatchMsg in tests.
func executeCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var lastMsg tea.Msg
		for _, subCmd := range batch {
			m := executeCmd(subCmd)
			if m != nil {
				lastMsg = m
			}
		}
		return lastMsg
	}
	return msg
}
