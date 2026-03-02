package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/admin"
	"github.com/gateixeira/gh-webhook-handler/internal/config"
	"github.com/gateixeira/gh-webhook-handler/internal/forwarder"
	"github.com/gateixeira/gh-webhook-handler/internal/reaper"
	"github.com/gateixeira/gh-webhook-handler/internal/retry"
	"github.com/gateixeira/gh-webhook-handler/internal/router"
	"github.com/gateixeira/gh-webhook-handler/internal/store"
	"github.com/gateixeira/gh-webhook-handler/internal/webhook"
)

func main() {
	configPath := flag.String("config", "configs/", "Path to route configuration directory")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "deliveries.db", "Path to SQLite database")
	webhookSecret := flag.String("webhook-secret", "", "GitHub App webhook secret (or set GITHUB_WEBHOOK_SECRET)")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *webhookSecret == "" {
		*webhookSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}
	if *webhookSecret == "" {
		slog.Error("webhook secret is required (--webhook-secret or GITHUB_WEBHOOK_SECRET)")
		os.Exit(1)
	}

	// Load route configuration
	cfg, err := config.LoadDir(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	slog.Info("loaded routes", "count", cfg.RouteCount())

	// Initialize delivery store
	deliveryStore, err := store.NewSQLite(*dbPath)
	if err != nil {
		slog.Error("failed to initialize store", "error", err)
		os.Exit(1)
	}
	defer deliveryStore.Close()

	// Initialize components
	eventRouter := router.New(cfg)
	eventForwarder := forwarder.New(deliveryStore)
	retryEngine := retry.NewEngine(deliveryStore, eventForwarder)
	webhookHandler := webhook.NewHandler(*webhookSecret, eventRouter, eventForwarder, deliveryStore)
	adminAPI := admin.NewAPI(deliveryStore, cfg)

	// Build HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", webhookHandler.HandleWebhook)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	adminAPI.RegisterRoutes(mux)

	server := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start retry engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go retryEngine.Start(ctx)

	// Start delivery reaper
	deliveryReaper := reaper.New(deliveryStore, reaper.DefaultConfig())
	go deliveryReaper.Start(ctx)

	// Start config watcher
	watcher := config.NewWatcher(*configPath, cfg, eventRouter)
	go watcher.Start(ctx)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		slog.Info("shutting down")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	slog.Info("starting server", "addr", *addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
