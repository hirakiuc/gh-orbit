package api

import (
	"log/slog"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	_ "modernc.org/sqlite"
)

type mockNotifier struct {
	calls []string
}

func (m *mockNotifier) Notify(title, subtitle, body, url string, priority int) error {
	m.calls = append(m.calls, title)
	return nil
}

func (m *mockNotifier) Shutdown() {}

func (m *mockNotifier) Status() BridgeStatus {
	return StatusHealthy
}

func TestAlertService_Throttling(t *testing.T) {
	logger := slog.Default()
	database, _ := db.OpenInMemory(logger)
	defer func() { _ = database.Close() }()

	cfg := &config.Config{}
	cfg.Notifications.Enabled = true

	notifier := &mockNotifier{}
	service := &AlertService{
		config:         cfg,
		db:             database,
		logger:         logger,
		notifier:       notifier,
		syncRepoCounts: make(map[string]int),
	}

	// 1. Silent Initial Baseline Test
	service.SyncStart() // Should detect empty DB
	if !service.isInitializing {
		t.Error("Expected isInitializing to be true for empty DB")
	}

	err := service.Notify(GHNotification{
		ID: "1",
		Repository: struct {
			FullName string `json:"full_name"`
		}{FullName: "repo/a"},
		Subject: struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Type   string `json:"type"`
			NodeID string `json:"node_id"`
		}{Title: "T1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(notifier.calls) > 0 {
		t.Error("Expected zero alerts during initial baseline sync")
	}

	// 2. Throttling Test (Seed DB to end baseline)
	_ = database.UpsertNotification(db.Notification{GitHubID: "seed", UpdatedAt: time.Now()})
	service.SyncStart()
	if service.isInitializing {
		t.Error("Expected isInitializing to be false after seeding DB")
	}

	// Send 10 notifications
	for i := 1; i <= 10; i++ {
		_ = service.Notify(GHNotification{
			ID: "id",
			Repository: struct {
				FullName string `json:"full_name"`
			}{FullName: "repo/a"},
			Subject: struct {
				Title  string `json:"title"`
				URL    string `json:"url"`
				Type   string `json:"type"`
				NodeID string `json:"node_id"`
			}{Title: "Notification"},
		})
	}

	// Expect 5 individual alerts + 1 summary alert = 6 total
	if len(notifier.calls) != 6 {
		t.Errorf("Expected 6 native alerts due to throttling, got %d", len(notifier.calls))
	}
	if notifier.calls[5] != "New Notifications" {
		t.Errorf("Expected summary alert as the 6th call, got %s", notifier.calls[5])
	}
}
