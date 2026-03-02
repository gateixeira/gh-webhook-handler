package circuitbreaker

import (
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State string

const (
	StateClosed   State = "closed"    // normal operation
	StateOpen     State = "open"      // failing, skip deliveries
	StateHalfOpen State = "half_open" // probing with one request
)

// defaults
const (
	DefaultFailureThreshold = 5
	DefaultCooldownPeriod   = 5 * time.Minute
)

// CircuitInfo holds the current state of a single circuit for external inspection.
type CircuitInfo struct {
	URL                 string    `json:"url"`
	State               State     `json:"state"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastFailure         time.Time `json:"last_failure,omitempty"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
	OpenedAt            time.Time `json:"opened_at,omitempty"`
}

// circuit tracks state for a single destination URL.
type circuit struct {
	state               State
	consecutiveFailures int
	lastFailure         time.Time
	lastSuccess         time.Time
	openedAt            time.Time
}

// Breaker manages circuit breakers for multiple destination URLs.
type Breaker struct {
	mu               sync.RWMutex
	circuits         map[string]*circuit
	failureThreshold int
	cooldownPeriod   time.Duration
}

// New creates a Breaker with default settings.
func New() *Breaker {
	return &Breaker{
		circuits:         make(map[string]*circuit),
		failureThreshold: DefaultFailureThreshold,
		cooldownPeriod:   DefaultCooldownPeriod,
	}
}

// NewWithConfig creates a Breaker with custom settings.
func NewWithConfig(failureThreshold int, cooldownPeriod time.Duration) *Breaker {
	if failureThreshold <= 0 {
		failureThreshold = DefaultFailureThreshold
	}
	if cooldownPeriod <= 0 {
		cooldownPeriod = DefaultCooldownPeriod
	}
	return &Breaker{
		circuits:         make(map[string]*circuit),
		failureThreshold: failureThreshold,
		cooldownPeriod:   cooldownPeriod,
	}
}

// Allow checks if a request to the given URL should proceed.
// Returns true if the circuit is closed or transitioning to half-open for a probe.
// Returns false if the circuit is open (destination is unreachable).
func (b *Breaker) Allow(url string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	c, exists := b.circuits[url]
	if !exists {
		return true // no circuit = closed = allow
	}

	switch c.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if cooldown has elapsed — transition to half-open
		if time.Since(c.openedAt) >= b.cooldownPeriod {
			c.state = StateHalfOpen
			return true // allow one probe request
		}
		return false
	case StateHalfOpen:
		// Already probing — don't allow additional requests
		return false
	default:
		return true
	}
}

// RecordSuccess records a successful delivery to the given URL.
// Resets the circuit to closed state.
func (b *Breaker) RecordSuccess(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	c, exists := b.circuits[url]
	if !exists {
		return // no circuit to update
	}

	c.state = StateClosed
	c.consecutiveFailures = 0
	c.lastSuccess = time.Now()
}

// RecordFailure records a failed delivery to the given URL.
// Opens the circuit after reaching the failure threshold.
func (b *Breaker) RecordFailure(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	c, exists := b.circuits[url]
	if !exists {
		c = &circuit{state: StateClosed}
		b.circuits[url] = c
	}

	c.consecutiveFailures++
	c.lastFailure = time.Now()

	if c.state == StateHalfOpen {
		// Probe failed — go back to open
		c.state = StateOpen
		c.openedAt = time.Now()
		return
	}

	if c.consecutiveFailures >= b.failureThreshold {
		c.state = StateOpen
		c.openedAt = time.Now()
	}
}

// Reset manually closes the circuit for the given URL.
func (b *Breaker) Reset(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if c, exists := b.circuits[url]; exists {
		c.state = StateClosed
		c.consecutiveFailures = 0
	}
}

// List returns the current state of all tracked circuits.
func (b *Breaker) List() []CircuitInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	infos := make([]CircuitInfo, 0, len(b.circuits))
	for url, c := range b.circuits {
		infos = append(infos, CircuitInfo{
			URL:                 url,
			State:               c.state,
			ConsecutiveFailures: c.consecutiveFailures,
			LastFailure:         c.lastFailure,
			LastSuccess:         c.lastSuccess,
			OpenedAt:            c.openedAt,
		})
	}
	return infos
}

// GetState returns the state of a specific circuit. Returns StateClosed if not tracked.
func (b *Breaker) GetState(url string) State {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if c, exists := b.circuits[url]; exists {
		return c.state
	}
	return StateClosed
}
