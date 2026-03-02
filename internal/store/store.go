package store

import "time"

// Delivery represents a webhook delivery record.
type Delivery struct {
	ID             string
	RouteName      string
	EventType      string
	SourceOrg      string
	SourceRepo     string
	DeliveryID     string // GitHub delivery ID from X-GitHub-Delivery header
	PayloadHash    string // SHA256 of the payload for deduplication
	Payload        []byte // Original webhook payload for retry
	DestinationURL string
	Status         string // "pending", "success", "failed", "retrying"
	ResponseCode   int
	ResponseBody   string
	Attempt        int
	MaxAttempts    int
	NextRetryAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ListFilter defines optional filters for listing deliveries.
type ListFilter struct {
	RouteName  string
	EventType  string
	Status     string
	SourceRepo string
	Limit      int
	Offset     int
}

// Store is the interface for delivery persistence.
type Store interface {
	Create(d *Delivery) error
	Update(d *Delivery) error
	Get(id string) (*Delivery, error)
	List(filter ListFilter) ([]Delivery, error)
	GetRetryable(now time.Time) ([]Delivery, error)
	Close() error
}
