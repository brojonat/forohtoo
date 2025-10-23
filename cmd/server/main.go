package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/metrics"
	"github.com/brojonat/forohtoo/service/server"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Load and validate configuration from environment
	// This fails fast if any required config is missing or invalid
	cfg := config.MustLoad()

	// Setup structured logging
	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting server",
		"addr", cfg.ServerAddr,
		"log_level", cfg.LogLevel,
	)

	// Setup context with cancellation for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database connection pool
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	// Verify database connection
	if err := dbPool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Initialize database store
	store := db.NewStore(dbPool)

	// Initialize Prometheus metrics collector
	metricsCollector := metrics.NewMetrics(nil) // nil uses default registry
	logger.Info("Prometheus metrics collector initialized")

	// Initialize Temporal client for schedule management
	temporalClient, err := temporal.NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		logger,
	)
	if err != nil {
		logger.Error("failed to create temporal client", "error", err)
		os.Exit(1)
	}
	defer temporalClient.Close()
	logger.Info("connected to temporal", "host", cfg.TemporalHost, "namespace", cfg.TemporalNamespace)

	// Initialize SSE publisher for streaming transactions
	ssePublisher, err := server.NewSSEPublisher(cfg.NATSURL, store, logger)
	if err != nil {
		logger.Error("failed to create SSE publisher", "error", err)
		os.Exit(1)
	}
	defer ssePublisher.Close()
	logger.Info("connected to NATS for SSE streaming", "url", cfg.NATSURL)

	// Initialize HTTP server with scheduler, SSE publisher, and metrics
	httpServer := server.New(cfg.ServerAddr, cfg, store, temporalClient, ssePublisher, metricsCollector, logger)

	// Enable HTML template rendering from embedded files
	if err := httpServer.WithTemplates(); err != nil {
		logger.Warn("failed to load HTML templates, web pages disabled", "error", err)
	}

	logger.Info("HTTP server initialized, all dependencies ready")

	// Start HTTP server in background
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting HTTP server", "addr", cfg.ServerAddr)
		serverErrors <- httpServer.Start()
	}()

	// Wait for shutdown signal or server error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("HTTP server error", "error", err)
		os.Exit(1)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig.String())

		// Graceful shutdown with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		logger.Info("stopping HTTP server")
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown HTTP server gracefully", "error", err)
			os.Exit(1)
		}

		logger.Info("HTTP server shutdown complete")
	}
}

// setupLogger creates a structured logger with the given log level.
func setupLogger(levelStr string) *slog.Logger {
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
