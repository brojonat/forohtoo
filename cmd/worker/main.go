package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/brojonat/forohtoo/service/solana"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Load and validate configuration from environment
	cfg := config.MustLoad()

	// Setup structured logging
	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting temporal worker",
		"temporal_host", cfg.TemporalHost,
		"namespace", cfg.TemporalNamespace,
		"task_queue", cfg.TemporalTaskQueue,
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

	// Initialize Solana RPC client
	solanaRPC := solana.NewRPCClient(cfg.SolanaRPCURL)
	solanaClient := solana.NewClient(solanaRPC, logger)
	logger.Info("initialized solana RPC client", "url", cfg.SolanaRPCURL)

	// Initialize NATS publisher
	natsPublisher, err := natspkg.NewPublisher(cfg.NATSURL, logger)
	if err != nil {
		logger.Error("failed to create NATS publisher", "error", err)
		os.Exit(1)
	}
	defer natsPublisher.Close()
	logger.Info("connected to NATS", "url", cfg.NATSURL)

	// Initialize Temporal worker
	workerConfig := temporal.WorkerConfig{
		TemporalHost:      cfg.TemporalHost,
		TemporalNamespace: cfg.TemporalNamespace,
		TaskQueue:         cfg.TemporalTaskQueue,
		Store:             store,
		SolanaClient:      solanaClient,
		Publisher:         natsPublisher,
		Logger:            logger,
	}

	worker, err := temporal.NewWorker(workerConfig)
	if err != nil {
		logger.Error("failed to create temporal worker", "error", err)
		os.Exit(1)
	}

	logger.Info("temporal worker initialized, all dependencies ready",
		"solana_rpc", cfg.SolanaRPCURL,
		"temporal_host", cfg.TemporalHost,
		"temporal_namespace", cfg.TemporalNamespace,
		"task_queue", cfg.TemporalTaskQueue,
	)

	// Start worker in background
	workerErrors := make(chan error, 1)
	go func() {
		logger.Info("starting temporal worker")
		workerErrors <- worker.Start()
	}()

	// Wait for shutdown signal or worker error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-workerErrors:
		logger.Error("temporal worker error", "error", err)
		os.Exit(1)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig.String())

		// Stop worker gracefully
		logger.Info("stopping temporal worker")
		worker.Stop()
		logger.Info("temporal worker stopped")

		logger.Info("shutdown complete")
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
