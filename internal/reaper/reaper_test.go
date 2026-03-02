package reaper

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/store"
	_ "modernc.org/sqlite"
)

func TestReaperCleansUpOldDeliveries(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer s.Close()

	deliveries := []store.Delivery{
		{ID: "old-success", RouteName: "r", EventType: "push", DestinationURL: "https://a.com", Status: "success", Attempt: 1, MaxAttempts: 3},
		{ID: "new-success", RouteName: "r", EventType: "push", DestinationURL: "https://b.com", Status: "success", Attempt: 1, MaxAttempts: 3},
		{ID: "old-failed", RouteName: "r", EventType: "push", DestinationURL: "https://c.com", Status: "permanently_failed", Payload: []byte("data"), Attempt: 3, MaxAttempts: 3},
	}
	for i := range deliveries {
		if err := s.Create(&deliveries[i]); err != nil {
			t.Fatalf("Create %s: %v", deliveries[i].ID, err)
		}
	}

	// Backdate old records via raw SQL
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer db.Close()

	oldTime := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := db.Exec("UPDATE deliveries SET updated_at = ? WHERE id IN ('old-success', 'old-failed')", oldTime); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	r := New(s, Config{
		Interval:            time.Hour,
		SuccessMaxAge:       24 * time.Hour,
		FailedPayloadMaxAge: 24 * time.Hour,
		FailedMaxAge:        72 * time.Hour,
	})
	r.RunOnce()

	// old-success should be deleted
	if _, err := s.Get("old-success"); err == nil {
		t.Error("old-success should have been deleted")
	}

	// new-success should remain
	d, err := s.Get("new-success")
	if err != nil || d == nil {
		t.Error("new-success should still exist")
	}

	// old-failed should still exist but payload cleared
	d, err = s.Get("old-failed")
	if err != nil || d == nil {
		t.Fatal("old-failed should still exist")
	}
	if d.Payload != nil {
		t.Error("old-failed payload should be nil")
	}
}
