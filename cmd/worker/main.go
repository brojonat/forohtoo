package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	forohtoo "github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/metrics"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/brojonat/forohtoo/service/solana"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// Initialize Prometheus metrics collector
	metricsCollector := metrics.NewMetrics(nil) // nil uses default registry
	logger.Info("Prometheus metrics collector initialized")

	// Start metrics HTTP server
	metricsAddr := getEnv("METRICS_ADDR", ":9091")
	metricsServer := &http.Server{
		Addr:    metricsAddr,
		Handler: promhttp.Handler(),
	}

	go func() {
		logger.Info("starting metrics HTTP server", "addr", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("failed to shutdown metrics server", "error", err)
		}
	}()

	// Initialize Mainnet Solana RPC client with multi-endpoint support
	mainnetClient := solana.NewClient(cfg.SolanaMainnetRPCURLs, metricsCollector, logger)
	logger.Info("initialized mainnet solana RPC client with multi-endpoint support",
		"total_endpoints", len(cfg.SolanaMainnetRPCURLs),
	)

	// Initialize Devnet Solana RPC client with multi-endpoint support
	devnetClient := solana.NewClient(cfg.SolanaDevnetRPCURLs, metricsCollector, logger)
	logger.Info("initialized devnet solana RPC client with multi-endpoint support",
		"total_endpoints", len(cfg.SolanaDevnetRPCURLs),
	)

	// Initialize NATS publisher
	natsPublisher, err := natspkg.NewPublisher(cfg.NATSURL, logger)
	if err != nil {
		logger.Error("failed to create NATS publisher", "error", err)
		os.Exit(1)
	}
	defer natsPublisher.Close()
	logger.Info("connected to NATS", "url", cfg.NATSURL)

	// Initialize forohtoo client for awaiting payment transactions
	forohtooClient := forohtoo.NewClient(cfg.ForohtooServerURL, nil, logger) // nil = default http.Client
	logger.Info("initialized forohtoo client", "server_url", cfg.ForohtooServerURL)

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
	logger.Info("connected to temporal for schedule management",
		"host", cfg.TemporalHost,
		"namespace", cfg.TemporalNamespace,
	)

	// Initialize Temporal worker
	workerConfig := temporal.WorkerConfig{
		TemporalHost:           cfg.TemporalHost,
		TemporalNamespace:      cfg.TemporalNamespace,
		TaskQueue:              cfg.TemporalTaskQueue,
		USDCMainnetMintAddress: cfg.USDCMainnetMintAddress,
		USDCDevnetMintAddress:  cfg.USDCDevnetMintAddress,
		Store:                  store,
		MainnetClient:          mainnetClient,
		DevnetClient:           devnetClient,
		Publisher:              natsPublisher,
		ForohtooClient:         forohtooClient,
		TemporalClient:         temporalClient,
		Metrics:                metricsCollector,
		Logger:                 logger,
	}

	worker, err := temporal.NewWorker(workerConfig)
	if err != nil {
		logger.Error("failed to create temporal worker", "error", err)
		os.Exit(1)
	}

	logger.Info("temporal worker initialized, all dependencies ready",
		"mainnet_total_endpoints", len(cfg.SolanaMainnetRPCURLs),
		"devnet_total_endpoints", len(cfg.SolanaDevnetRPCURLs),
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

// extractEndpointFromURL extracts a short identifier from the Solana RPC URL for metrics labeling.
// Examples:
//   - "https://api.mainnet-beta.solana.com" -> "mainnet"
//   - "https://api.devnet.solana.com" -> "devnet"
//   - "https://mainnet.helius-rpc.com/?api-key=..." -> "helius"
//   - "https://some-endpoint.quiknode.pro/..." -> "quiknode"
func extractEndpointFromURL(rpcURL string) string {
	parsed, err := url.Parse(rpcURL)
	if err != nil {
		return "unknown"
	}

	host := parsed.Hostname()

	// Check for common RPC providers
	if contains(host, "helius") {
		return "helius"
	}
	if contains(host, "quiknode") || contains(host, "quicknode") {
		return "quiknode"
	}
	if contains(host, "alchemy") {
		return "alchemy"
	}
	if contains(host, "triton") {
		return "triton"
	}
	if contains(host, "rpcpool") {
		return "rpcpool"
	}

	// Check for official Solana endpoints
	if contains(host, "mainnet") {
		return "mainnet"
	}
	if contains(host, "devnet") {
		return "devnet"
	}
	if contains(host, "testnet") {
		return "testnet"
	}

	// Fallback to hostname
	return host
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) != -1
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// getEnv returns the value of an environment variable or a default if not set.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
