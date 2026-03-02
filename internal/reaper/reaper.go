package reaper

import (
	"context"
	"log/slog"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/store"
)

// Config holds retention policy settings for the reaper.
type Config struct {
	// Interval between reaper runs.
	Interval time.Duration
	// SuccessMaxAge is how long to keep successful deliveries before deleting.
	SuccessMaxAge time.Duration
	// FailedPayloadMaxAge is how long to keep payloads on permanently_failed records before clearing.
	FailedPayloadMaxAge time.Duration
	// FailedMaxAge is how long to keep permanently_failed records before deleting entirely.
	FailedMaxAge time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Interval:            1 * time.Hour,
		SuccessMaxAge:       7 * 24 * time.Hour,  // 7 days
		FailedPayloadMaxAge: 3 * 24 * time.Hour,  // 3 days
		FailedMaxAge:        30 * 24 * time.Hour,  // 30 days
	}
}

// Reaper periodically cleans up old delivery records from the store.
type Reaper struct {
	store store.Store
	cfg   Config
}

// New creates a Reaper with the given store and config.
func New(s store.Store, cfg Config) *Reaper {
	return &Reaper{store: s, cfg: cfg}
}

// Start runs the reaper loop until ctx is cancelled.
func (r *Reaper) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("reaper: stopped")
			return
		case <-ticker.C:
			r.run()
		}
	}
}

// RunOnce executes a single reaper pass. Useful for testing.
func (r *Reaper) RunOnce() {
	r.run()
}

func (r *Reaper) run() {
	now := time.Now().UTC()

	// 1. Delete old successful deliveries.
	if n, err := r.store.DeleteOlderThan("success", now.Add(-r.cfg.SuccessMaxAge)); err != nil {
		slog.Error("reaper: failed to delete old successful deliveries", "error", err)
	} else if n > 0 {
		slog.Info("reaper: deleted old successful deliveries", "count", n)
	}

	// 2. Clear payloads from old permanently_failed deliveries.
	if n, err := r.store.ClearPayloadsOlderThan("permanently_failed", now.Add(-r.cfg.FailedPayloadMaxAge)); err != nil {
		slog.Error("reaper: failed to clear old failed payloads", "error", err)
	} else if n > 0 {
		slog.Info("reaper: cleared payloads from old permanently_failed deliveries", "count", n)
	}

	// 3. Delete very old permanently_failed deliveries.
	if n, err := r.store.DeleteOlderThan("permanently_failed", now.Add(-r.cfg.FailedMaxAge)); err != nil {
		slog.Error("reaper: failed to delete old permanently_failed deliveries", "error", err)
	} else if n > 0 {
		slog.Info("reaper: deleted old permanently_failed deliveries", "count", n)
	}

	// 4. Clear payloads from stale circuit_open deliveries (destination never recovered).
	if n, err := r.store.ClearPayloadsOlderThan("circuit_open", now.Add(-r.cfg.FailedPayloadMaxAge)); err != nil {
		slog.Error("reaper: failed to clear old circuit_open payloads", "error", err)
	} else if n > 0 {
		slog.Info("reaper: cleared payloads from stale circuit_open deliveries", "count", n)
	}

	// 5. Delete very old circuit_open deliveries.
	if n, err := r.store.DeleteOlderThan("circuit_open", now.Add(-r.cfg.FailedMaxAge)); err != nil {
		slog.Error("reaper: failed to delete old circuit_open deliveries", "error", err)
	} else if n > 0 {
		slog.Info("reaper: deleted old circuit_open deliveries", "count", n)
	}

	// 6. Delete old expired deliveries.
	if n, err := r.store.DeleteOlderThan("expired", now.Add(-r.cfg.SuccessMaxAge)); err != nil {
		slog.Error("reaper: failed to delete old expired deliveries", "error", err)
	} else if n > 0 {
		slog.Info("reaper: deleted old expired deliveries", "count", n)
	}
}
