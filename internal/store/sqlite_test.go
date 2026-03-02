package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)

	retry := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	d := &Delivery{
		ID:             "d-1",
		RouteName:      "route-a",
		EventType:      "push",
		SourceOrg:      "org1",
		SourceRepo:     "repo1",
		DeliveryID:     "gh-123",
		PayloadHash:    "abc123",
		DestinationURL: "https://example.com/hook",
		Status:         "pending",
		ResponseCode:   0,
		ResponseBody:   "",
		Attempt:        1,
		MaxAttempts:    3,
		NextRetryAt:    &retry,
	}

	if err := s.Create(d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get("d-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != "d-1" || got.RouteName != "route-a" || got.EventType != "push" {
		t.Errorf("unexpected delivery fields: %+v", got)
	}
	if got.SourceOrg != "org1" || got.SourceRepo != "repo1" {
		t.Errorf("unexpected source fields: %+v", got)
	}
	if got.DeliveryID != "gh-123" || got.PayloadHash != "abc123" {
		t.Errorf("unexpected delivery/hash fields: %+v", got)
	}
	if got.Status != "pending" || got.Attempt != 1 || got.MaxAttempts != 3 {
		t.Errorf("unexpected status fields: %+v", got)
	}
	if got.NextRetryAt == nil {
		t.Fatal("NextRetryAt should not be nil")
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps should not be zero: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestListWithFilters(t *testing.T) {
	s := newTestStore(t)

	deliveries := []Delivery{
		{ID: "d-1", RouteName: "route-a", EventType: "push", DestinationURL: "https://a.com", Status: "success", SourceRepo: "repo1", Attempt: 1, MaxAttempts: 3},
		{ID: "d-2", RouteName: "route-b", EventType: "push", DestinationURL: "https://b.com", Status: "failed", SourceRepo: "repo2", Attempt: 1, MaxAttempts: 3},
		{ID: "d-3", RouteName: "route-a", EventType: "pull_request", DestinationURL: "https://c.com", Status: "success", SourceRepo: "repo1", Attempt: 1, MaxAttempts: 3},
	}
	for i := range deliveries {
		if err := s.Create(&deliveries[i]); err != nil {
			t.Fatalf("Create %s: %v", deliveries[i].ID, err)
		}
	}

	tests := []struct {
		name  string
		filter ListFilter
		want  int
	}{
		{"no filter", ListFilter{}, 3},
		{"by route", ListFilter{RouteName: "route-a"}, 2},
		{"by event", ListFilter{EventType: "push"}, 2},
		{"by status", ListFilter{Status: "failed"}, 1},
		{"by repo", ListFilter{SourceRepo: "repo1"}, 2},
		{"combined", ListFilter{RouteName: "route-a", Status: "success"}, 2},
		{"limit", ListFilter{Limit: 1}, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(tc.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("List(%+v) returned %d deliveries, want %d", tc.filter, len(got), tc.want)
			}
		})
	}
}

func TestGetRetryable(t *testing.T) {
	s := newTestStore(t)

	past := time.Now().UTC().Add(-1 * time.Hour)
	future := time.Now().UTC().Add(1 * time.Hour)

	deliveries := []Delivery{
		// eligible: failed, past retry, attempt < max
		{ID: "d-1", RouteName: "r", EventType: "push", DestinationURL: "https://a.com", Status: "failed", Attempt: 1, MaxAttempts: 3, NextRetryAt: &past},
		// not eligible: status is success
		{ID: "d-2", RouteName: "r", EventType: "push", DestinationURL: "https://b.com", Status: "success", Attempt: 1, MaxAttempts: 3, NextRetryAt: &past},
		// not eligible: next_retry_at in future
		{ID: "d-3", RouteName: "r", EventType: "push", DestinationURL: "https://c.com", Status: "failed", Attempt: 1, MaxAttempts: 3, NextRetryAt: &future},
		// not eligible: attempt >= max_attempts
		{ID: "d-4", RouteName: "r", EventType: "push", DestinationURL: "https://d.com", Status: "failed", Attempt: 3, MaxAttempts: 3, NextRetryAt: &past},
	}
	for i := range deliveries {
		if err := s.Create(&deliveries[i]); err != nil {
			t.Fatalf("Create %s: %v", deliveries[i].ID, err)
		}
	}

	got, err := s.GetRetryable(time.Now().UTC())
	if err != nil {
		t.Fatalf("GetRetryable: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetRetryable returned %d deliveries, want 1", len(got))
	}
	if got[0].ID != "d-1" {
		t.Errorf("GetRetryable returned %s, want d-1", got[0].ID)
	}
}

func TestUpdateChangesStatusAndUpdatedAt(t *testing.T) {
	s := newTestStore(t)

	d := &Delivery{
		ID:             "d-1",
		RouteName:      "route-a",
		EventType:      "push",
		DestinationURL: "https://a.com",
		Status:         "pending",
		Attempt:        1,
		MaxAttempts:    3,
	}
	if err := s.Create(d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	before, err := s.Get("d-1")
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}

	// Small sleep to ensure updated_at changes
	time.Sleep(1100 * time.Millisecond)

	d.Status = "success"
	d.ResponseCode = 200
	d.ResponseBody = "OK"
	if err := s.Update(d); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := s.Get("d-1")
	if err != nil {
		t.Fatalf("Get after: %v", err)
	}

	if after.Status != "success" {
		t.Errorf("status = %q, want %q", after.Status, "success")
	}
	if after.ResponseCode != 200 {
		t.Errorf("response_code = %d, want 200", after.ResponseCode)
	}
	if !after.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("updated_at did not advance: before=%v after=%v", before.UpdatedAt, after.UpdatedAt)
	}
}
