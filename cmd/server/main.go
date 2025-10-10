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
	"github.com/brojonat/forohtoo/service/server"
	"github.com/brojonat/forohtoo/service/solana"
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

	// Initialize Solana RPC client
	// Note: For premium RPC endpoints, include API key in the URL
	solanaRPC := solana.NewRPCClient(cfg.SolanaRPCURL)
	solanaClient := solana.NewClient(solanaRPC, logger)
	logger.Info("initialized solana RPC client", "url", cfg.SolanaRPCURL)

	// Initialize HTTP server
	httpServer := server.New(cfg.ServerAddr, store, logger)

	// TODO: Initialize NATS connection
	// TODO: Initialize Temporal client
	// TODO: Initialize poller service

	logger.Info("server initialized, all dependencies ready",
		"solana_rpc", cfg.SolanaRPCURL,
		"nats_url", cfg.NATSURL,
		"temporal_host", cfg.TemporalHost,
	)

	// Prevent unused variable errors until we implement the full service
	_ = solanaClient

	// Start HTTP server in background
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.Start()
	}()

	// Wait for shutdown signal or server error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig.String())

		// Graceful shutdown with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown server gracefully", "error", err)
			os.Exit(1)
		}

		logger.Info("server shutdown complete")
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
