package config

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultPollInterval is the default interval between config directory polls.
const DefaultPollInterval = 30 * time.Second

// Reloadable is implemented by components that need to react to config changes.
type Reloadable interface {
	Reload(cfg *Config)
}

// Watcher polls a config directory and triggers reloads when files change.
type Watcher struct {
	dirPath     string
	cfg         *Config
	subscribers []Reloadable
	interval    time.Duration
	lastMod     time.Time
}

// NewWatcher creates a Watcher for the given directory.
func NewWatcher(dirPath string, cfg *Config, subscribers ...Reloadable) *Watcher {
	return &Watcher{
		dirPath:     dirPath,
		cfg:         cfg,
		subscribers: subscribers,
		interval:    DefaultPollInterval,
	}
}

// Start begins polling until the context is cancelled.
func (w *Watcher) Start(ctx context.Context) {
	// Capture initial mtime so we don't reload immediately.
	w.lastMod = w.latestMtime()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			latest := w.latestMtime()
			if latest.After(w.lastMod) {
				w.lastMod = latest
				w.reload()
			}
		}
	}
}

// latestMtime returns the most recent modification time among YAML files in
// the watched directory.
func (w *Watcher) latestMtime() time.Time {
	var latest time.Time
	entries, err := os.ReadDir(w.dirPath)
	if err != nil {
		log.Printf("watcher: error reading directory %s: %v", w.dirPath, err)
		return latest
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func (w *Watcher) reload() {
	newCfg, err := LoadDir(w.dirPath)
	if err != nil {
		log.Printf("watcher: failed to reload config: %v", err)
		return
	}
	w.cfg.UpdateRoutes(newCfg.Routes())
	log.Printf("watcher: reloaded %d routes", w.cfg.RouteCount())

	for _, s := range w.subscribers {
		s.Reload(w.cfg)
	}
}
