package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gateixeira/gh-webhook-handler/internal/router"
)

// ForwardResult holds the outcome of a forwarding attempt.
type ForwardResult struct {
	Route      router.MatchedRoute
	StatusCode int
	Err        error
}

// RouterInterface matches incoming events to configured routes.
type RouterInterface interface {
	Match(org, repo, eventType string) []router.MatchedRoute
}

// ForwarderInterface forwards webhook payloads to matched destinations.
type ForwarderInterface interface {
	Forward(ctx context.Context, route router.MatchedRoute, eventType string, deliveryID string, payload []byte) *ForwardResult
}

// StoreInterface persists webhook delivery records.
type StoreInterface interface{}

// Handler processes incoming GitHub webhook requests.
type Handler struct {
	secret    string
	router    RouterInterface
	forwarder ForwarderInterface
	store     StoreInterface
}

// NewHandler creates a new webhook Handler.
func NewHandler(secret string, router RouterInterface, forwarder ForwarderInterface, store StoreInterface) *Handler {
	return &Handler{
		secret:    secret,
		router:    router,
		forwarder: forwarder,
		store:     store,
	}
}

// HandleWebhook is the HTTP handler for incoming GitHub webhooks.
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 10 MB.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	signature := r.Header.Get("X-Hub-Signature-256")
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	if !ValidateSignature(payload, signature, h.secret) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "invalid signature",
		})
		return
	}

	org, repo := parseOrgRepo(payload)

	routes := h.router.Match(org, repo, eventType)

	// Fire-and-forget forwarding for each matched route.
	for _, route := range routes {
		go h.forwarder.Forward(context.Background(), route, eventType, deliveryID, payload)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"received":       true,
		"matched_routes": len(routes),
	})
}

// parseOrgRepo extracts the org and repo from the webhook payload.
// It first looks at repository.full_name ("org/repo"), then falls back to organization.login.
func parseOrgRepo(payload []byte) (org, repo string) {
	var body struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}

	if err := json.Unmarshal(payload, &body); err != nil {
		return "", ""
	}

	if body.Repository.FullName != "" {
		parts := strings.SplitN(body.Repository.FullName, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	if body.Organization.Login != "" {
		return body.Organization.Login, ""
	}

	return "", ""
}
