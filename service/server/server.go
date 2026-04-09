package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/helius"
	"github.com/brojonat/forohtoo/service/metrics"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the HTTP server for the wallet service.
type Server struct {
	addr           string
	cfg            *config.Config
	store          *db.Store
	temporalClient *temporal.Client   // only used for payment gateway workflows
	heliusClient   *helius.Client     // manages Helius webhook address list
	natsPublisher  natspkg.Publisher   // publishes webhook-received transactions to NATS
	ssePublisher   *SSEPublisher
	renderer       *TemplateRenderer
	metrics        *metrics.Metrics
	logger         *slog.Logger
	server         *http.Server
}

// New creates a new HTTP server with the given dependencies.
// The temporalClient is only used for payment gateway workflows (optional).
// The heliusClient manages webhook address lists for transaction monitoring.
// The natsPublisher is used by the webhook handler to publish events.
// The ssePublisher is optional - if nil, SSE endpoints won't be available.
func New(addr string, cfg *config.Config, store *db.Store, temporalClient *temporal.Client, heliusClient *helius.Client, natsPublisher natspkg.Publisher, ssePublisher *SSEPublisher, m *metrics.Metrics, logger *slog.Logger) *Server {
	return &Server{
		addr:           addr,
		cfg:            cfg,
		store:          store,
		temporalClient: temporalClient,
		heliusClient:   heliusClient,
		natsPublisher:  natsPublisher,
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
	mux.Handle("POST /api/v1/wallet-assets", handleRegisterWalletAsset(s.store, s.heliusClient, s.temporalClient, s.cfg, s.logger))
	mux.Handle("DELETE /api/v1/wallet-assets/{address}", handleUnregisterWalletAsset(s.store, s.heliusClient, s.logger))
	mux.Handle("GET /api/v1/wallet-assets/{address}", handleGetWalletAsset(s.store, s.logger))
	mux.Handle("GET /api/v1/wallet-assets", handleListWalletAssets(s.store, s.logger))
	mux.Handle("GET /api/v1/transactions", handleListTransactions(s.store, s.logger))

	// Helius webhook endpoint (receives push notifications from Helius)
	mux.Handle("POST /api/v1/webhooks/helius", handleHeliusWebhook(s.store, s.natsPublisher, s.cfg.HeliusWebhookAuthToken, s.logger))

	// Payment gateway routes (uses Temporal for workflow orchestration)
	if s.temporalClient != nil {
		mux.Handle("GET /api/v1/registration-status/{workflow_id}", handleGetRegistrationStatus(s.temporalClient, s.logger))
	}

	// SSE streaming endpoints (if SSE publisher is configured)
	if s.ssePublisher != nil {
		mux.Handle("GET /api/v1/stream/transactions/{address}", handleStreamTransactions(s.ssePublisher, s.logger))
		mux.Handle("GET /api/v1/stream/transactions", handleStreamTransactions(s.ssePublisher, s.logger))
		s.logger.Info("SSE streaming endpoints enabled")
	}

	// HTML pages (if template renderer is configured)
	if s.renderer != nil {
		mux.HandleFunc("GET /", handleSSEClientPage(s.renderer))
		mux.HandleFunc("GET /stream", handleSSEClientPage(s.renderer))
		mux.HandleFunc("GET /favicon.ico", handleFavicon())
		mux.HandleFunc("GET /favicon.svg", handleFavicon())
	}

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Prometheus metrics endpoint
	if s.metrics != nil {
		mux.Handle("GET /metrics", promhttp.Handler())
	}

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
// when the payment gateway is enabled.
func (s *Server) ensureServiceWalletRegistered(ctx context.Context) error {
	if !s.cfg.PaymentGateway.Enabled {
		return nil
	}

	serviceWallet := s.cfg.PaymentGateway.ServiceWallet
	serviceNetwork := s.cfg.PaymentGateway.ServiceNetwork
	assetType := "spl-token"
	var tokenMint string
	if serviceNetwork == "mainnet" {
		tokenMint = s.cfg.USDCMainnetMintAddress
	} else {
		tokenMint = s.cfg.USDCDevnetMintAddress
	}

	exists, err := s.store.WalletExists(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
	if err != nil {
		return fmt.Errorf("failed to check service wallet existence: %w", err)
	}
	if exists {
		return nil
	}

	s.logger.Info("registering service wallet for USDC payment monitoring",
		"address", serviceWallet,
		"network", serviceNetwork,
	)

	ataAddr, err := computeAssociatedTokenAddress(serviceWallet, tokenMint)
	if err != nil {
		return fmt.Errorf("failed to compute service wallet ATA: %w", err)
	}
	ata := &ataAddr

	_, err = s.store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                serviceWallet,
		Network:                serviceNetwork,
		AssetType:              assetType,
		TokenMint:              tokenMint,
		AssociatedTokenAddress: ata,
		PollInterval:           s.cfg.DefaultPollInterval,
		Status:                 "active",
	})
	if err != nil {
		return fmt.Errorf("failed to register service wallet: %w", err)
	}

	// Add the ATA to the Helius webhook
	if s.heliusClient != nil {
		if err := s.heliusClient.AddAddress(ctx, *ata); err != nil {
			s.store.DeleteWallet(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
			return fmt.Errorf("failed to add service wallet to Helius webhook: %w", err)
		}
	}

	s.logger.Info("service wallet registered", "address", serviceWallet, "network", serviceNetwork)
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.ssePublisher != nil {
		s.ssePublisher.Close()
	}
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// corsMiddleware adds CORS headers to all responses and handles OPTIONS preflight requests.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
