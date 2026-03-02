package circuitbreaker

import (
	"testing"
	"time"
)

func TestClosedAllowsRequests(t *testing.T) {
	b := New()
	if !b.Allow("https://example.com") {
		t.Error("closed circuit should allow requests")
	}
}

func TestOpensAfterThreshold(t *testing.T) {
	b := New() // threshold = 5
	url := "https://failing.com"

	for i := 0; i < DefaultFailureThreshold; i++ {
		if !b.Allow(url) {
			t.Fatalf("circuit should allow request before threshold (attempt %d)", i+1)
		}
		b.RecordFailure(url)
	}

	// Circuit should now be open
	if b.Allow(url) {
		t.Error("circuit should be open after threshold failures")
	}
	if b.GetState(url) != StateOpen {
		t.Errorf("state = %s, want open", b.GetState(url))
	}
}

func TestOpenToHalfOpenAfterCooldown(t *testing.T) {
	b := NewWithConfig(2, 50*time.Millisecond) // low threshold and fast cooldown for testing
	url := "https://slow.com"

	b.RecordFailure(url)
	b.RecordFailure(url)

	if b.Allow(url) {
		t.Error("should be open immediately after failures")
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open and allow one probe
	if !b.Allow(url) {
		t.Error("should allow probe request after cooldown")
	}
	if b.GetState(url) != StateHalfOpen {
		t.Errorf("state = %s, want half_open", b.GetState(url))
	}

	// Second request while half-open should be blocked
	if b.Allow(url) {
		t.Error("should block additional requests while half-open")
	}
}

func TestHalfOpenToClosedOnSuccess(t *testing.T) {
	b := NewWithConfig(2, 50*time.Millisecond)
	url := "https://recovering.com"

	b.RecordFailure(url)
	b.RecordFailure(url)
	time.Sleep(60 * time.Millisecond)

	b.Allow(url) // transition to half-open
	b.RecordSuccess(url) // probe succeeded

	if b.GetState(url) != StateClosed {
		t.Errorf("state = %s, want closed after successful probe", b.GetState(url))
	}
	if !b.Allow(url) {
		t.Error("should allow requests after circuit closes")
	}
}

func TestHalfOpenToOpenOnFailure(t *testing.T) {
	b := NewWithConfig(2, 50*time.Millisecond)
	url := "https://still-failing.com"

	b.RecordFailure(url)
	b.RecordFailure(url)
	time.Sleep(60 * time.Millisecond)

	b.Allow(url) // transition to half-open
	b.RecordFailure(url) // probe failed

	if b.GetState(url) != StateOpen {
		t.Errorf("state = %s, want open after failed probe", b.GetState(url))
	}
	if b.Allow(url) {
		t.Error("should block requests after failed probe")
	}
}

func TestSuccessResetsConsecutiveFailures(t *testing.T) {
	b := NewWithConfig(3, time.Minute)
	url := "https://flaky.com"

	b.RecordFailure(url)
	b.RecordFailure(url)
	// 2 failures, below threshold
	b.RecordSuccess(url)
	// Counter reset

	b.RecordFailure(url)
	b.RecordFailure(url)
	// 2 failures again, still below threshold
	if b.GetState(url) != StateClosed {
		t.Error("should still be closed — success reset the counter")
	}
}

func TestResetManuallyClosesCircuit(t *testing.T) {
	b := New()
	url := "https://admin-reset.com"

	for i := 0; i < DefaultFailureThreshold; i++ {
		b.RecordFailure(url)
	}
	if b.GetState(url) != StateOpen {
		t.Fatal("should be open")
	}

	b.Reset(url)
	if b.GetState(url) != StateClosed {
		t.Error("should be closed after reset")
	}
	if !b.Allow(url) {
		t.Error("should allow requests after reset")
	}
}

func TestListReturnsAllCircuits(t *testing.T) {
	b := New()
	b.RecordFailure("https://a.com")
	b.RecordFailure("https://b.com")

	infos := b.List()
	if len(infos) != 2 {
		t.Errorf("List returned %d circuits, want 2", len(infos))
	}
}

func TestUnknownURLReturnsClosed(t *testing.T) {
	b := New()
	if b.GetState("https://never-seen.com") != StateClosed {
		t.Error("unknown URL should return closed state")
	}
}
