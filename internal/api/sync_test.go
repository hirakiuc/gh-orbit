package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/db"
	_ "modernc.org/sqlite"
)

func TestParseLinkHeader(t *testing.T) {
	header := `<https://api.github.com/resource?page=2>; rel="next", <https://api.github.com/resource?page=5>; rel="last"`
	links := parseLinkHeader(header)

	if links["next"] != "https://api.github.com/resource?page=2" {
		t.Errorf("Expected next link, got %s", links["next"])
	}
	if links["last"] != "https://api.github.com/resource?page=5" {
		t.Errorf("Expected last link, got %s", links["last"])
	}
}

type mockFetcher struct {
	notifs []GHNotification
	meta   *db.SyncMeta
	err    error
	called bool
}

func (m *mockFetcher) FetchNotifications(meta *db.SyncMeta) ([]GHNotification, *db.SyncMeta, int, error) {
	m.called = true
	return m.notifs, m.meta, 5000, m.err
}

func TestSyncEngine_Sync(t *testing.T) {
	logger := slog.Default()
	database, err := db.OpenInMemory(logger)
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	defer func() { _ = database.Close() }()

	userID := "user-1"
	notifs := []GHNotification{
		{ID: "1", Subject: struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Type   string `json:"type"`
			NodeID string `json:"node_id"`
		}{Title: "T1", URL: "U1", Type: "PullRequest", NodeID: "N1"}, Reason: "mention", Repository: struct {
			FullName string `json:"full_name"`
		}{FullName: "R1"}, UpdatedAt: time.Now()},
	}

	fetcher := &mockFetcher{
		notifs: notifs,
		meta: &db.SyncMeta{
			UserID:       userID,
			Key:          "notifications",
			PollInterval: 60,
		},
	}

	engine := NewSyncEngine(fetcher, database, nil, logger)

	// 1. Initial Sync (Force=true)
	if _, err := engine.Sync(userID, true); err != nil {
		t.Fatalf("Initial sync failed: %v", err)
	}

	// Verify persistence
	list, _ := database.ListNotifications()
	if len(list) != 1 {
		t.Errorf("Expected 1 notification, got %d", len(list))
	}

	// 2. Automated Sync too soon (Force=false)
	fetcher.called = false
	if _, err := engine.Sync(userID, false); err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}
	if fetcher.called {
		t.Error("Expected automated sync to be skipped due to interval")
	}

	// 3. Forced Sync (Force=true)
	fetcher.called = false
	if _, err := engine.Sync(userID, true); err != nil {
		t.Fatalf("Forced sync failed: %v", err)
	}
	if !fetcher.called {
		t.Error("Expected forced sync to bypass interval check")
	}
}

func TestConditionalRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "etag-123" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "etag-123")
		_ = json.NewEncoder(w).Encode([]GHNotification{})
	}))
	defer ts.Close()

	client := &Client{
		http:    ts.Client(),
		baseURL: ts.URL + "/",
	}
	
	fetcher := NewNotificationFetcher(client, slog.Default())
	meta := &db.SyncMeta{ETag: "etag-123"}
	
	_, _, _, err := fetcher.FetchNotifications(meta)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
}
