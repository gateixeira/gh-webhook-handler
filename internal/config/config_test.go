package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `routes:
  - name: "ci-trigger"
    description: "Forward push events to CI"
    sources:
      - org: "my-org"
        repos: ["api-service", "web-app"]
    events:
      - push
      - pull_request
    destination:
      url: "https://ci.example.com/hook"
      secret: "$CI_SECRET"
      headers:
        X-Source: "handler"
    retry:
      max_attempts: 5
      backoff: "exponential"
  - name: "security-alerts"
    description: "Forward security events"
    sources:
      - org: "my-org"
        repos: []
    events:
      - code_scanning_alert
    destination:
      url: "https://siem.example.com/ingest"
    retry:
      max_attempts: 3
      backoff: "linear"
`

func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "routes.yaml", sampleYAML)

	t.Setenv("CI_SECRET", "supersecret")

	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error: %v", err)
	}

	if cfg.RouteCount() != 2 {
		t.Fatalf("expected 2 routes, got %d", cfg.RouteCount())
	}

	routes := cfg.Routes()
	if routes[0].Name != "ci-trigger" {
		t.Errorf("expected route name ci-trigger, got %s", routes[0].Name)
	}
	if routes[0].Destination.Secret != "supersecret" {
		t.Errorf("expected resolved secret, got %q", routes[0].Destination.Secret)
	}
	if routes[0].Retry.Backoff != "exponential" {
		t.Errorf("expected exponential backoff, got %s", routes[0].Retry.Backoff)
	}
}

func TestFindRoutesExactMatch(t *testing.T) {
	cfg := NewConfig([]Route{
		{
			Name:    "r1",
			Sources: []Source{{Org: "org1", Repos: []string{"repo-a"}}},
			Events:  []string{"push"},
			Destination: Destination{URL: "https://example.com"},
		},
	})

	matched := cfg.FindRoutes("org1", "repo-a", "push")
	if len(matched) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matched))
	}
	if matched[0].Name != "r1" {
		t.Errorf("expected r1, got %s", matched[0].Name)
	}
}

func TestFindRoutesWildcardRepos(t *testing.T) {
	cfg := NewConfig([]Route{
		{
			Name:    "wildcard",
			Sources: []Source{{Org: "org1", Repos: nil}},
			Events:  []string{"push"},
			Destination: Destination{URL: "https://example.com"},
		},
	})

	matched := cfg.FindRoutes("org1", "any-repo", "push")
	if len(matched) != 1 {
		t.Fatalf("expected 1 match for wildcard repos, got %d", len(matched))
	}
}

func TestFindRoutesNoMatch(t *testing.T) {
	cfg := NewConfig([]Route{
		{
			Name:    "r1",
			Sources: []Source{{Org: "org1", Repos: []string{"repo-a"}}},
			Events:  []string{"push"},
			Destination: Destination{URL: "https://example.com"},
		},
	})

	// Wrong org
	if len(cfg.FindRoutes("org2", "repo-a", "push")) != 0 {
		t.Error("expected no match for wrong org")
	}
	// Wrong repo
	if len(cfg.FindRoutes("org1", "repo-b", "push")) != 0 {
		t.Error("expected no match for wrong repo")
	}
	// Wrong event
	if len(cfg.FindRoutes("org1", "repo-a", "release")) != 0 {
		t.Error("expected no match for wrong event")
	}
}

func TestValidationMissingName(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - description: "no name"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://example.com"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
}

func TestValidationDuplicateName(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "dup.yaml", `routes:
  - name: "dup"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://a.com"
  - name: "dup"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://b.com"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for duplicate name")
	}
}

func TestValidationMissingSources(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - name: "no-src"
    events: [push]
    destination:
      url: "https://example.com"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for missing sources")
	}
}

func TestValidationMissingEvents(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - name: "no-events"
    sources:
      - org: "o"
    destination:
      url: "https://example.com"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for missing events")
	}
}

func TestValidationMissingURL(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - name: "no-url"
    sources:
      - org: "o"
    events: [push]
    destination:
      secret: "$X"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for missing destination URL")
	}
}

func TestValidationInvalidBackoff(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - name: "bad-backoff"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://example.com"
    retry:
      backoff: "random"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for invalid backoff")
	}
}

func TestValidMaxAge(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "ok.yaml", `routes:
  - name: "with-max-age"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://example.com"
    retry:
      max_age: "2h"
`)
	_, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("expected no error for valid max_age, got: %v", err)
	}
}

func TestValidationInvalidMaxAge(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yaml", `routes:
  - name: "bad-max-age"
    sources:
      - org: "o"
    events: [push]
    destination:
      url: "https://example.com"
    retry:
      max_age: "invalid"
`)
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatal("expected validation error for invalid max_age")
	}
}
