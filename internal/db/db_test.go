package db

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *DB {
	// Use in-memory database for tests
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	instance := &DB{db}
	if err := instance.migrate(); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	return instance
}

func TestUpsertAndGetNotification(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	now := time.Now().Truncate(time.Second)
	n := Notification{
		GitHubID:           "123",
		SubjectTitle:       "Test PR",
		SubjectURL:         "https://api.github.com/repos/user/repo/pulls/1",
		SubjectType:        "PullRequest",
		Reason:             "author",
		RepositoryFullName: "user/repo",
		HTMLURL:            "https://github.com/user/repo/pull/1",
		IsEnriched:         false,
		UpdatedAt:          now,
	}

	// Test Insert
	if err := db.UpsertNotification(n); err != nil {
		t.Fatalf("UpsertNotification failed: %v", err)
	}

	// Test Retrieve
	ns, err := db.GetNotification("123")
	if err != nil {
		t.Fatalf("GetNotification failed: %v", err)
	}
	if ns == nil {
		t.Fatal("Notification not found")
	}

	if ns.GitHubID != n.GitHubID || ns.SubjectType != n.SubjectType {
		t.Errorf("Expected %v, got %v", n, ns.Notification)
	}

	// Verify orbit_state was automatically created
	if ns.Priority != 0 || ns.Status != "entry" {
		t.Errorf("Unexpected orbit state: %+v", ns.OrbitState)
	}

	// Test Update Orbit State
	newState := OrbitState{
		NotificationID: "123",
		Priority:       2,
		Status:         "tracking",
		IsReadLocally:  true,
	}
	if err := db.UpdateOrbitState(newState); err != nil {
		t.Fatalf("UpdateOrbitState failed: %v", err)
	}

	ns2, _ := db.GetNotification("123")
	if ns2.Priority != 2 || ns2.Status != "tracking" || !ns2.IsReadLocally {
		t.Errorf("Orbit state not updated correctly: %+v", ns2.OrbitState)
	}

	// Test Upsert (Update)
	n.SubjectTitle = "Updated PR Title"
	if err := db.UpsertNotification(n); err != nil {
		t.Fatalf("Upsert (Update) failed: %v", err)
	}

	ns3, _ := db.GetNotification("123")
	if ns3.SubjectTitle != "Updated PR Title" {
		t.Errorf("Title not updated: %s", ns3.SubjectTitle)
	}
	// Verify orbit state was preserved
	if ns3.Priority != 2 {
		t.Errorf("Orbit state lost during notification update")
	}
}

func TestUpsertPreservesLocalState(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	id := "state-test-1"
	n := Notification{
		GitHubID:     id,
		SubjectTitle: "Original Title",
		SubjectURL:   "https://api.github.com/repos/user/repo/pulls/1",
		UpdatedAt:    time.Now().Truncate(time.Second),
	}

	// 1. Initial sync
	if err := db.UpsertNotification(n); err != nil {
		t.Fatal(err)
	}

	// 2. User sets local state
	err := db.UpdateOrbitState(OrbitState{
		NotificationID: id,
		Priority:       3,
		Status:         "tracking",
		IsReadLocally:  true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Differential sync (API update)
	n.SubjectTitle = "Updated Title"
	n.UpdatedAt = n.UpdatedAt.Add(time.Hour)
	if err := db.UpsertNotification(n); err != nil {
		t.Fatal(err)
	}

	// 4. Verify title updated BUT local state remains
	ns, err := db.GetNotification(id)
	if err != nil {
		t.Fatal(err)
	}

	if ns.SubjectTitle != "Updated Title" {
		t.Errorf("Title not updated: %s", ns.SubjectTitle)
	}
	if ns.Priority != 3 || ns.Status != "tracking" || !ns.IsReadLocally {
		t.Errorf("Local state was overwritten: %+v", ns.OrbitState)
	}
}

func TestListNotifications(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	n1 := Notification{GitHubID: "1", SubjectTitle: "N1", SubjectType: "Issue", UpdatedAt: time.Now()}
	n2 := Notification{GitHubID: "2", SubjectTitle: "N2", SubjectType: "PullRequest", UpdatedAt: time.Now().Add(time.Hour)}

	_ = db.UpsertNotification(n1)
	_ = db.UpsertNotification(n2)

	list, err := db.ListNotifications()
	if err != nil {
		t.Fatalf("ListNotifications failed: %v", err)
	}

	if len(list) != 2 {
		t.Errorf("Expected 2 notifications, got %d", len(list))
	}

	// Should be ordered by updated_at DESC
	if list[0].GitHubID != "2" {
		t.Errorf("Expected notification '2' first due to sorting")
	}
}
