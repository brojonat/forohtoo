package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/metrics"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the HTTP server for the wallet service.
type Server struct {
	addr           string
	cfg            *config.Config
	store          *db.Store
	scheduler      temporal.Scheduler
	temporalClient *temporal.Client
	ssePublisher   *SSEPublisher
	renderer       *TemplateRenderer
	metrics        *metrics.Metrics
	logger         *slog.Logger
	server         *http.Server
}

// New creates a new HTTP server with the given dependencies.
// The scheduler is used to create/delete Temporal schedules for wallet polling.
// The temporalClient is used for starting workflows and querying workflow status.
// The ssePublisher is optional - if nil, SSE endpoints won't be available.
// The renderer is optional - if nil, HTML endpoints won't be available.
// The metrics is optional - if nil, metrics endpoints won't be available.
func New(addr string, cfg *config.Config, store *db.Store, scheduler temporal.Scheduler, temporalClient *temporal.Client, ssePublisher *SSEPublisher, m *metrics.Metrics, logger *slog.Logger) *Server {
	return &Server{
		addr:           addr,
		cfg:            cfg,
		store:          store,
		scheduler:      scheduler,
		temporalClient: temporalClient,
		ssePublisher:   ssePublisher,
		metrics:        m,
		logger:         logger,
	}
}

// WithTemplates adds template rendering support to the server using embedded files
func (s *Server) WithTemplates() error {
	renderer, err := NewTemplateRenderer(s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize templates: %w", err)
	}
	s.renderer = renderer
	s.logger.Info("HTML templates loaded from embedded files")
	return nil
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	// Ensure service wallet is registered if payment gateway is enabled
	if err := s.ensureServiceWalletRegistered(context.Background()); err != nil {
		return fmt.Errorf("failed to ensure service wallet registered: %w", err)
	}

	mux := http.NewServeMux()

	// Wallet asset routes
	mux.Handle("POST /api/v1/wallet-assets", handleRegisterWalletAsset(s.store, s.scheduler, s.temporalClient, s.cfg, s.logger))
	mux.Handle("DELETE /api/v1/wallet-assets/{address}", handleUnregisterWalletAsset(s.store, s.scheduler, s.logger))
	mux.Handle("GET /api/v1/wallet-assets/{address}", handleGetWalletAsset(s.store, s.logger))
	mux.Handle("GET /api/v1/wallet-assets", handleListWalletAssets(s.store, s.logger))
	mux.Handle("GET /api/v1/transactions", handleListTransactions(s.store, s.logger))

	// Payment gateway routes
	mux.Handle("GET /api/v1/registration-status/{workflow_id}", handleGetRegistrationStatus(s.temporalClient, s.logger))

	// SSE streaming endpoints (if SSE publisher is configured)
	if s.ssePublisher != nil {
		mux.Handle("GET /api/v1/stream/transactions/{address}", handleStreamTransactions(s.ssePublisher, s.logger))
		mux.Handle("GET /api/v1/stream/transactions", handleStreamTransactions(s.ssePublisher, s.logger))
		s.logger.Info("SSE streaming endpoints enabled")
	} else {
		s.logger.Warn("SSE publisher not configured, streaming endpoints disabled")
	}

	// HTML pages (if template renderer is configured)
	if s.renderer != nil {
		mux.HandleFunc("GET /", handleSSEClientPage(s.renderer))
		mux.HandleFunc("GET /stream", handleSSEClientPage(s.renderer))
		mux.HandleFunc("GET /favicon.ico", handleFavicon())
		mux.HandleFunc("GET /favicon.svg", handleFavicon())
		s.logger.Info("HTML page endpoints enabled")
	}

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Prometheus metrics endpoint (if metrics collector is configured)
	if s.metrics != nil {
		mux.Handle("GET /metrics", promhttp.Handler())
		s.logger.Info("Prometheus metrics endpoint enabled")
	}

	// Wrap mux with CORS middleware
	handler := corsMiddleware(mux)

	s.server = &http.Server{
		Addr:         s.addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.logger.Info("starting HTTP server", "addr", s.addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

// ensureServiceWalletRegistered ensures the service wallet is registered for monitoring
// when the payment gateway is enabled. This allows the server to receive payment notifications.
func (s *Server) ensureServiceWalletRegistered(ctx context.Context) error {
	if !s.cfg.PaymentGateway.Enabled {
		s.logger.Debug("payment gateway disabled, skipping service wallet registration")
		return nil
	}

	serviceWallet := s.cfg.PaymentGateway.ServiceWallet
	serviceNetwork := s.cfg.PaymentGateway.ServiceNetwork

	// Service wallet always monitors USDC payments
	assetType := "spl-token"
	var tokenMint string
	if serviceNetwork == "mainnet" {
		tokenMint = s.cfg.USDCMainnetMintAddress
	} else {
		tokenMint = s.cfg.USDCDevnetMintAddress
	}

	// Check if service wallet is already registered
	exists, err := s.store.WalletExists(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
	if err != nil {
		return fmt.Errorf("failed to check service wallet existence: %w", err)
	}

	if exists {
		s.logger.Info("service wallet already registered",
			"address", serviceWallet,
			"network", serviceNetwork,
			"asset_type", assetType,
		)
		return nil
	}

	// Register service wallet
	s.logger.Info("registering service wallet for USDC payment monitoring",
		"address", serviceWallet,
		"network", serviceNetwork,
		"usdc_mint", tokenMint,
	)

	// Compute ATA for USDC
	ataAddr, err := computeAssociatedTokenAddress(serviceWallet, tokenMint)
	if err != nil {
		return fmt.Errorf("failed to compute service wallet ATA: %w", err)
	}
	ata := &ataAddr

	// Use a reasonable poll interval for service wallet (30s default)
	pollInterval := s.cfg.DefaultPollInterval
	if pollInterval == 0 {
		pollInterval = 30 * time.Second
	}

	// Create wallet in database
	wallet, err := s.store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                serviceWallet,
		Network:                serviceNetwork,
		AssetType:              assetType,
		TokenMint:              tokenMint,
		AssociatedTokenAddress: ata,
		PollInterval:           pollInterval,
		Status:                 "active",
	})
	if err != nil {
		return fmt.Errorf("failed to register service wallet: %w", err)
	}

	// Create Temporal schedule
	if err := s.scheduler.UpsertWalletAssetSchedule(ctx, serviceWallet, serviceNetwork, assetType, tokenMint, ata, pollInterval); err != nil {
		// Rollback wallet creation
		s.store.DeleteWallet(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
		return fmt.Errorf("failed to create schedule for service wallet: %w", err)
	}

	s.logger.Info("service wallet registered successfully",
		"address", wallet.Address,
		"network", serviceNetwork,
		"asset_type", assetType,
		"poll_interval", wallet.PollInterval,
	)

	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")

	// Close SSE publisher first (disconnects all clients)
	if s.ssePublisher != nil {
		s.ssePublisher.Close()
	}

	// Then shutdown HTTP server
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware adds CORS headers to all responses and handles OPTIONS preflight requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for all requests
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		// Handle preflight OPTIONS requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Pass through to next handler
		next.ServeHTTP(w, r)
	})
}
