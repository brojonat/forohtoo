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
	"github.com/brojonat/forohtoo/service/helius"
	"github.com/brojonat/forohtoo/service/metrics"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/brojonat/forohtoo/service/server"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.MustLoad()

	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting server", "addr", cfg.ServerAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()
	if err := dbPool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	store := db.NewStore(dbPool)

	// Prometheus metrics
	metricsCollector := metrics.NewMetrics(nil)

	// Helius webhook client (primary ingestion path)
	heliusClient := helius.NewClient(cfg.HeliusAPIKey, cfg.HeliusWebhookURL, cfg.HeliusWebhookAuthToken, logger)
	if err := heliusClient.EnsureWebhooks(ctx); err != nil {
		logger.Error("failed to initialize Helius webhooks", "error", err)
		os.Exit(1)
	}
	logger.Info("Helius webhook integration ready", "webhook_id", heliusClient.WebhookID())

	// NATS publisher (for webhook handler to publish transaction events)
	natsPublisher, err := natspkg.NewPublisher(cfg.NATSURL, logger)
	if err != nil {
		logger.Error("failed to create NATS publisher", "error", err)
		os.Exit(1)
	}
	defer natsPublisher.Close()

	// SSE publisher (for streaming to clients)
	ssePublisher, err := server.NewSSEPublisher(cfg.NATSURL, store, logger)
	if err != nil {
		logger.Error("failed to create SSE publisher", "error", err)
		os.Exit(1)
	}
	defer ssePublisher.Close()

	// Temporal client (only needed for payment gateway workflows)
	var temporalClient *temporal.Client
	if cfg.PaymentGateway.Enabled {
		tc, err := temporal.NewClient(cfg.TemporalHost, cfg.TemporalNamespace, cfg.TemporalTaskQueue, logger)
		if err != nil {
			logger.Error("failed to create temporal client", "error", err)
			os.Exit(1)
		}
		defer tc.Close()
		temporalClient = tc
		logger.Info("temporal client ready (payment gateway)")
	}

	httpServer := server.New(cfg.ServerAddr, cfg, store, temporalClient, heliusClient, natsPublisher, ssePublisher, metricsCollector, logger)

	if err := httpServer.WithTemplates(); err != nil {
		logger.Warn("failed to load HTML templates", "error", err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.Start()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("HTTP server error", "error", err)
		os.Exit(1)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig.String())
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown gracefully", "error", err)
			os.Exit(1)
		}
	}
}

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
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
