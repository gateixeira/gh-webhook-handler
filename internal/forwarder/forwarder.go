package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/circuitbreaker"
	"github.com/gateixeira/gh-webhook-handler/internal/router"
	"github.com/gateixeira/gh-webhook-handler/internal/store"
	"github.com/gateixeira/gh-webhook-handler/internal/webhook"
	"github.com/google/uuid"
)

const (
	forwardTimeout   = 30 * time.Second
	retryBase        = 10 * time.Second
	retryFixedBase   = 30 * time.Second
	maxRetryInterval = 1 * time.Hour
)

// Result holds the outcome of a forwarding attempt.
type Result struct {
	Success      bool
	StatusCode   int
	ResponseBody string
	Error        error
}

// Forwarder forwards webhook payloads to destination URLs.
type Forwarder struct {
	client  *http.Client
	store   store.Store
	breaker *circuitbreaker.Breaker
}

// New creates a new Forwarder with the given store and circuit breaker.
func New(s store.Store, breaker *circuitbreaker.Breaker) *Forwarder {
	return &Forwarder{
		client: &http.Client{
			Timeout: forwardTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		store:   s,
		breaker: breaker,
	}
}

// Forward sends the webhook payload to the route's destination and records the delivery.
func (f *Forwarder) Forward(ctx context.Context, route router.MatchedRoute, eventType string, deliveryID string, payload []byte) *webhook.ForwardResult {
	// Check circuit breaker — skip HTTP call if destination is unreachable,
	// but still store the delivery with payload for later retry.
	if !f.breaker.Allow(route.DestinationURL) {
		nr := time.Now().Add(5 * time.Minute) // retry after cooldown period
		var expiresAt *time.Time
		if route.MaxAge > 0 {
			t := time.Now().Add(route.MaxAge)
			expiresAt = &t
		}
		d := &store.Delivery{
			ID:             uuid.New().String(),
			RouteName:      route.Name,
			EventType:      eventType,
			DeliveryID:     deliveryID,
			Payload:        payload,
			DestinationURL: route.DestinationURL,
			Status:         "circuit_open",
			Attempt:        0,
			MaxAttempts:    route.MaxAttempts,
			ExpiresAt:      expiresAt,
			NextRetryAt:    &nr,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		_ = f.store.Create(d)

		return &webhook.ForwardResult{
			Route:      route,
			StatusCode: 0,
			Err:        fmt.Errorf("circuit open for %s", route.DestinationURL),
		}
	}

	result := f.doForward(ctx, route, eventType, deliveryID, payload)

	// Record success/failure in circuit breaker.
	if result.Success {
		f.breaker.RecordSuccess(route.DestinationURL)
	} else {
		f.breaker.RecordFailure(route.DestinationURL)
	}

	// Record delivery in store.
	status := "success"
	var nextRetry *time.Time
	if !result.Success {
		status = "failed"
		nr := calculateNextRetry(route.Backoff, 1)
		nextRetry = &nr
	}

	// Only store payload for failed deliveries to minimize DB size growth.
	var storedPayload []byte
	if !result.Success {
		storedPayload = payload
	}

	var expiresAt *time.Time
	if route.MaxAge > 0 {
		t := time.Now().Add(route.MaxAge)
		expiresAt = &t
	}

	d := &store.Delivery{
		ID:             uuid.New().String(),
		RouteName:      route.Name,
		EventType:      eventType,
		DeliveryID:     deliveryID,
		Payload:        storedPayload,
		DestinationURL: route.DestinationURL,
		Status:         status,
		ResponseCode:   result.StatusCode,
		ResponseBody:   result.ResponseBody,
		Attempt:        1,
		MaxAttempts:    route.MaxAttempts,
		ExpiresAt:      expiresAt,
		NextRetryAt:    nextRetry,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	// Best-effort store; don't fail the forward on store errors.
	_ = f.store.Create(d)

	return &webhook.ForwardResult{
		Route:      route,
		StatusCode: result.StatusCode,
		Err:        result.Error,
	}
}

// Retry re-sends a previously failed delivery. It satisfies retry.ForwarderInterface.
func (f *Forwarder) Retry(ctx context.Context, delivery *store.Delivery) error {
	// Check circuit breaker — if still open, return error to reschedule.
	if !f.breaker.Allow(delivery.DestinationURL) {
		return fmt.Errorf("circuit open for %s", delivery.DestinationURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.DestinationURL, bytes.NewReader(delivery.Payload))
	if err != nil {
		return fmt.Errorf("creating retry request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", delivery.EventType)
	req.Header.Set("X-GitHub-Delivery", delivery.DeliveryID)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending retry request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	delivery.ResponseCode = resp.StatusCode
	delivery.ResponseBody = string(body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		f.breaker.RecordSuccess(delivery.DestinationURL)
		return nil
	}

	f.breaker.RecordFailure(delivery.DestinationURL)
	return fmt.Errorf("destination returned %d", resp.StatusCode)
}

func (f *Forwarder) doForward(ctx context.Context, route router.MatchedRoute, eventType string, deliveryID string, payload []byte) *Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, route.DestinationURL, bytes.NewReader(payload))
	if err != nil {
		return &Result{Error: fmt.Errorf("creating request: %w", err)}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", eventType)
	req.Header.Set("X-GitHub-Delivery", deliveryID)

	for k, v := range route.Headers {
		req.Header.Set(k, v)
	}

	if route.Secret != "" {
		req.Header.Set("X-Hub-Signature-256", SignPayload(payload, route.Secret))
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return &Result{Error: fmt.Errorf("sending request: %w", err)}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // Read up to 1 MB of response.

	return &Result{
		Success:      resp.StatusCode >= 200 && resp.StatusCode < 300,
		StatusCode:   resp.StatusCode,
		ResponseBody: string(body),
	}
}

// calculateNextRetry computes the next retry time based on backoff strategy.
func calculateNextRetry(backoff string, attempt int) time.Time {
	var delay time.Duration
	switch backoff {
	case "exponential":
		delay = retryBase * time.Duration(math.Pow(2, float64(attempt-1)))
	case "linear":
		delay = retryBase * time.Duration(attempt)
	default: // "fixed" or unrecognized
		delay = retryFixedBase
	}
	if delay > maxRetryInterval {
		delay = maxRetryInterval
	}
	return time.Now().Add(delay)
}
