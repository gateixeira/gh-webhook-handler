package router

import (
	"os"
	"strings"
	"sync"

	"github.com/gateixeira/gh-webhook-handler/internal/config"
)

// MatchedRoute represents a route that matched an incoming event.
type MatchedRoute struct {
	Name           string
	DestinationURL string
	Secret         string
	Headers        map[string]string
	MaxAttempts    int
	Backoff        string
}

// Router evaluates incoming events against the loaded configuration.
type Router struct {
	mu  sync.RWMutex
	cfg *config.Config
}

// New creates a Router backed by the given Config.
func New(cfg *config.Config) *Router {
	return &Router{cfg: cfg}
}

// Reload replaces the config reference (implements config.Reloadable).
func (r *Router) Reload(cfg *config.Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg
}

// Match returns all routes matching the given org, repo, and event type.
func (r *Router) Match(org, repo, eventType string) []MatchedRoute {
	r.mu.RLock()
	cfg := r.cfg
	r.mu.RUnlock()

	routes := cfg.FindRoutes(org, repo, eventType)

	matched := make([]MatchedRoute, 0, len(routes))
	for _, rt := range routes {
		matched = append(matched, MatchedRoute{
			Name:           rt.Name,
			DestinationURL: rt.Destination.URL,
			Secret:         resolveEnv(rt.Destination.Secret),
			Headers:        rt.Destination.Headers,
			MaxAttempts:    rt.Retry.MaxAttempts,
			Backoff:        rt.Retry.Backoff,
		})
	}
	return matched
}

// resolveEnv returns the value of the environment variable if s starts with "$",
// otherwise returns s unchanged.
func resolveEnv(s string) string {
	if strings.HasPrefix(s, "$") {
		return os.Getenv(s[1:])
	}
	return s
}
