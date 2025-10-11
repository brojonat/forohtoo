package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
)

// Server represents the HTTP server for the wallet service.
type Server struct {
	addr         string
	store        *db.Store
	scheduler    temporal.Scheduler
	ssePublisher *SSEPublisher
	renderer     *TemplateRenderer
	logger       *slog.Logger
	server       *http.Server
}

// New creates a new HTTP server with the given dependencies.
// The scheduler is used to create/delete Temporal schedules for wallet polling.
// The ssePublisher is optional - if nil, SSE endpoints won't be available.
// The renderer is optional - if nil, HTML endpoints won't be available.
func New(addr string, store *db.Store, scheduler temporal.Scheduler, ssePublisher *SSEPublisher, logger *slog.Logger) *Server {
	return &Server{
		addr:         addr,
		store:        store,
		scheduler:    scheduler,
		ssePublisher: ssePublisher,
		logger:       logger,
	}
}

// WithTemplates adds template rendering support to the server
func (s *Server) WithTemplates(templatesDir string) error {
	renderer, err := NewTemplateRenderer(templatesDir, s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize templates: %w", err)
	}
	s.renderer = renderer
	s.logger.Info("HTML templates loaded", "dir", templatesDir)
	return nil
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Register routes
	mux.Handle("POST /api/v1/wallets", handleRegisterWalletWithScheduler(s.store, s.scheduler, s.logger))
	mux.Handle("DELETE /api/v1/wallets/{address}", handleUnregisterWalletWithScheduler(s.store, s.scheduler, s.logger))
	mux.Handle("GET /api/v1/wallets/{address}", handleGetWallet(s.store, s.logger))
	mux.Handle("GET /api/v1/wallets", handleListWallets(s.store, s.logger))

	// SSE streaming endpoints (if SSE publisher is configured)
	if s.ssePublisher != nil {
		mux.Handle("GET /api/v1/stream/transactions/{address}", handleStreamTransactions(s.ssePublisher, s.logger))
		mux.Handle("GET /api/v1/stream/transactions", handleStreamAllTransactions(s.ssePublisher, s.logger))
		s.logger.Info("SSE streaming endpoints enabled")
	} else {
		s.logger.Warn("SSE publisher not configured, streaming endpoints disabled")
	}

	// HTML pages (if template renderer is configured)
	if s.renderer != nil {
		mux.HandleFunc("GET /", handleSSEClientPage(s.renderer))
		mux.HandleFunc("GET /stream", handleSSEClientPage(s.renderer))
		s.logger.Info("HTML page endpoints enabled")
	}

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

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
