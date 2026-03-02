package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// configFile mirrors the YAML structure on disk.
type configFile struct {
	Routes []Route `yaml:"routes"`
}

// validBackoffs is the set of accepted backoff strategies.
var validBackoffs = map[string]bool{
	"exponential": true,
	"linear":      true,
	"fixed":       true,
}

// LoadDir reads every .yaml/.yml file in dirPath, validates, and returns a
// merged Config.
func LoadDir(dirPath string) (*Config, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("reading config directory: %w", err)
	}

	var allRoutes []Route
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		routes, err := LoadFile(filepath.Join(dirPath, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("loading %s: %w", e.Name(), err)
		}
		allRoutes = append(allRoutes, routes...)
	}

	if err := validateRoutes(allRoutes); err != nil {
		return nil, err
	}

	resolveSecrets(allRoutes)

	return NewConfig(allRoutes), nil
}

// LoadFile parses a single YAML file and returns its routes (unvalidated).
func LoadFile(filePath string) ([]Route, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return cf.Routes, nil
}

// validateRoutes checks that every route satisfies the required constraints.
func validateRoutes(routes []Route) error {
	seen := make(map[string]bool)
	for i, r := range routes {
		if r.Name == "" {
			return fmt.Errorf("route %d: name is required", i)
		}
		if seen[r.Name] {
			return fmt.Errorf("route %q: duplicate name", r.Name)
		}
		seen[r.Name] = true

		if len(r.Sources) == 0 {
			return fmt.Errorf("route %q: at least one source is required", r.Name)
		}
		if len(r.Events) == 0 {
			return fmt.Errorf("route %q: at least one event is required", r.Name)
		}
		if r.Destination.URL == "" {
			return fmt.Errorf("route %q: destination URL is required", r.Name)
		}
		if r.Retry.Backoff != "" && !validBackoffs[r.Retry.Backoff] {
			return fmt.Errorf("route %q: backoff must be one of exponential, linear, fixed", r.Name)
		}
	}
	return nil
}

// resolveSecrets replaces $ENV_VAR references in secrets with their values.
func resolveSecrets(routes []Route) {
	for i := range routes {
		secret := routes[i].Destination.Secret
		if strings.HasPrefix(secret, "$") {
			envName := strings.TrimPrefix(secret, "$")
			val := os.Getenv(envName)
			if val == "" {
				log.Printf("WARNING: environment variable %s referenced by route %q is not set", envName, routes[i].Name)
			}
			routes[i].Destination.Secret = val
		}
	}
}
