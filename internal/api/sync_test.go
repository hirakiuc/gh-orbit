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
	header := `<https://api.github.com/user/repos?page=3&per_page=100>; rel="next", <https://api.github.com/user/repos?page=50&per_page=100>; rel="last"`
	links := parseLinkHeader(header)

	if links["next"] != "https://api.github.com/user/repos?page=3&per_page=100" {
		t.Errorf("Expected next link, got %s", links["next"])
	}
	if links["last"] != "https://api.github.com/user/repos?page=50&per_page=100" {
		t.Errorf("Expected last link, got %s", links["last"])
	}
}

func TestSyncEngine_Sync(t *testing.T) {
	// 1. Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notifications":
			// Check for conditional headers
			if r.Header.Get("If-Modified-Since") == "last-mod" {
				w.WriteHeader(http.StatusNotModified)
				return
			}

			w.Header().Set("Last-Modified", "new-mod")
			w.Header().Set("X-Poll-Interval", "10")
			_ = json.NewEncoder(w).Encode([]GHNotification{
				{
					ID: "notif-1",
					Repository: struct {
						FullName string `json:"full_name"`
					}{FullName: "owner/repo"},
					Subject: struct {
						Title string `json:"title"`
						URL   string `json:"url"`
						Type  string `json:"type"`
					}{Title: "PR Title", Type: "PullRequest"},
					UpdatedAt: time.Now().Truncate(time.Second),
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// 2. Setup Test DB
	database, err := db.OpenInMemory(slog.Default())
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer func() { _ = database.Close() }()

	// 3. Initialize SyncEngine
	client := &Client{
		http:    server.Client(),
		baseURL: server.URL + "/",
		host:    "github.com",
	}
	fetcher := NewNotificationFetcher(client, slog.Default())
	engine := NewSyncEngine(fetcher, database, nil, slog.Default())

	// 4. Run First Sync
	userID := "user-1"
	if err := engine.Sync(userID); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify notification persisted
	ns, err := database.GetNotification("notif-1")
	if err != nil {
		t.Fatal(err)
	}
	if ns == nil {
		t.Fatal("Notification not found in DB")
	}
	if ns.SubjectTitle != "PR Title" {
		t.Errorf("Expected PR Title, got %s", ns.SubjectTitle)
	}

	// Verify sync_meta updated
	meta, err := database.GetSyncMeta(userID, "notifications")
	if err != nil {
		t.Fatal(err)
	}
	if meta.LastModified != "new-mod" {
		t.Errorf("Expected last_modified 'new-mod', got %s", meta.LastModified)
	}
	if meta.PollInterval != 10 {
		t.Errorf("Expected poll_interval 10, got %d", meta.PollInterval)
	}

	// 5. Run Second Sync (should be 304 Not Modified due to LastModified header)
	if err := engine.Sync(userID); err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}

	// Since we mocked 304 if If-Modified-Since is present, and Sync sends it, it should work.
}
