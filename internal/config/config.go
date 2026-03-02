package config

import (
	"sync"
)

// Route defines a webhook forwarding rule.
type Route struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Sources     []Source    `yaml:"sources"`
	Events      []string    `yaml:"events"`
	Destination Destination `yaml:"destination"`
	Retry       RetryPolicy `yaml:"retry"`
}

// Source specifies which org/repos a route matches.
type Source struct {
	Org   string   `yaml:"org"`
	Repos []string `yaml:"repos"`
}

// Destination is the target endpoint for forwarded webhooks.
type Destination struct {
	URL     string            `yaml:"url"`
	Secret  string            `yaml:"secret"`
	Headers map[string]string `yaml:"headers"`
}

// RetryPolicy controls retry behaviour for failed deliveries.
type RetryPolicy struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`
}

// Config holds all loaded routes with thread-safe access.
type Config struct {
	mu     sync.RWMutex
	routes []Route
}

// NewConfig creates a Config from the given routes.
func NewConfig(routes []Route) *Config {
	return &Config{routes: routes}
}

// Routes returns a copy of the current route list.
func (c *Config) Routes() []Route {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Route, len(c.routes))
	copy(out, c.routes)
	return out
}

// RouteCount returns the number of loaded routes.
func (c *Config) RouteCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.routes)
}

// UpdateRoutes replaces all routes atomically (used during hot-reload).
func (c *Config) UpdateRoutes(routes []Route) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.routes = routes
}

// FindRoutes returns all routes matching the given org, repo, and event type.
// A route matches when:
//   - source.Org == org
//   - source.Repos is empty (wildcard) OR repo is in source.Repos
//   - eventType is in route.Events
func (c *Config) FindRoutes(org, repo, eventType string) []Route {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var matched []Route
	for _, r := range c.routes {
		if !containsString(r.Events, eventType) {
			continue
		}
		for _, src := range r.Sources {
			if src.Org != org {
				continue
			}
			if len(src.Repos) == 0 || containsString(src.Repos, repo) {
				matched = append(matched, r)
				break
			}
		}
	}
	return matched
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
