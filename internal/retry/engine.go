package retry

import (
	"context"
	"log"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/store"
)

// ForwarderInterface is implemented by any component that can re-send a
// webhook delivery. The Retry method receives the full Delivery record;
// the implementation is responsible for constructing the outbound HTTP
// request (destination URL, headers, payload, etc.).
//
type ForwarderInterface interface {
	Retry(ctx context.Context, delivery *store.Delivery) error
}

// Engine polls the store for retryable deliveries and re-forwards them.
type Engine struct {
	store     store.Store
	forwarder ForwarderInterface
}

// NewEngine creates a retry Engine.
func NewEngine(s store.Store, f ForwarderInterface) *Engine {
	return &Engine{store: s, forwarder: f}
}

const pollInterval = 10 * time.Second

// Start runs the retry polling loop. It blocks until ctx is cancelled.
func (e *Engine) Start(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("retry: engine stopped")
			return
		case <-ticker.C:
			e.processBatch(ctx)
		}
	}
}

func (e *Engine) processBatch(ctx context.Context) {
	deliveries, err := e.store.GetRetryable(time.Now())
	if err != nil {
		log.Printf("retry: failed to query retryable deliveries: %v", err)
		return
	}
	if len(deliveries) == 0 {
		return
	}

	log.Printf("retry: processing %d retryable deliveries", len(deliveries))

	for i := range deliveries {
		d := &deliveries[i]

		// Mark as retrying.
		d.Status = "retrying"
		d.UpdatedAt = time.Now()
		if err := e.store.Update(d); err != nil {
			log.Printf("retry: failed to mark delivery %s as retrying: %v", d.ID, err)
			continue
		}

		// Attempt re-delivery.
		if err := e.forwarder.Retry(ctx, d); err != nil {
			e.handleFailure(d, err)
			continue
		}

		// Success — clear payload to free storage.
		d.Status = "success"
		d.Payload = nil
		d.UpdatedAt = time.Now()
		if err := e.store.Update(d); err != nil {
			log.Printf("retry: failed to mark delivery %s as success: %v", d.ID, err)
		} else {
			log.Printf("retry: delivery %s succeeded on attempt %d", d.ID, d.Attempt)
		}
	}
}

func (e *Engine) handleFailure(d *store.Delivery, retryErr error) {
	d.Attempt++
	d.UpdatedAt = time.Now()

	if d.Attempt >= d.MaxAttempts {
		d.Status = "permanently_failed"
		log.Printf("retry: delivery %s permanently failed after %d attempts: %v", d.ID, d.Attempt, retryErr)
	} else {
		d.Status = "failed"
		backoff := CalculateBackoff("exponential", d.Attempt)
		next := time.Now().Add(backoff)
		d.NextRetryAt = &next
		log.Printf("retry: delivery %s failed (attempt %d/%d), next retry at %s: %v",
			d.ID, d.Attempt, d.MaxAttempts, next.Format(time.RFC3339), retryErr)
	}

	if err := e.store.Update(d); err != nil {
		log.Printf("retry: failed to update delivery %s after failure: %v", d.ID, err)
	}
}
