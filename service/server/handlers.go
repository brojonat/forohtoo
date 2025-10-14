package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5"
)

const (
	maxRequestBodySize = 1 << 20 // 1MB - plenty for wallet registration
	maxAddressLength   = 100     // Solana addresses are 44 chars, give buffer
	minPollInterval    = 10 * time.Second
	maxPollInterval    = 24 * time.Hour
)

var (
	// Valid Solana address characters: base58 (no 0, O, I, l)
	validAddressRegex = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]+$`)
)

// handleRegisterWallet returns a handler that registers a new wallet for monitoring.
// POST /api/v1/wallets
func handleRegisterWallet(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Limit request body size to prevent memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

		var req struct {
			Address      string `json:"address"`
			PollInterval string `json:"poll_interval"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Debug("failed to decode register request", "error", err)
			// Check if error is due to body size limit
			if strings.Contains(err.Error(), "http: request body too large") {
				writeError(w, "request body too large: maximum size is 1MB", http.StatusBadRequest)
				return
			}
			writeError(w, "invalid request body: must be valid JSON", http.StatusBadRequest)
			return
		}

		// Validate address
		if err := validateAddress(req.Address); err != nil {
			logger.Debug("invalid address", "address", req.Address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Parse and validate poll interval
		pollInterval, err := time.ParseDuration(req.PollInterval)
		if err != nil {
			logger.Debug("invalid poll interval", "interval", req.PollInterval, "error", err)
			writeError(w, "invalid poll_interval: must be a valid duration (e.g. '30s', '1m')", http.StatusBadRequest)
			return
		}

		if err := validatePollInterval(pollInterval); err != nil {
			logger.Debug("invalid poll interval value", "interval", pollInterval, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Create wallet
		params := db.CreateWalletParams{
			Address:      req.Address,
			PollInterval: pollInterval,
			Status:       "active",
		}

		wallet, err := store.CreateWallet(r.Context(), params)
		if err != nil {
			logger.Error("failed to create wallet", "address", req.Address, "error", err)
			// Check for duplicate key error
			if strings.Contains(err.Error(), "duplicate key") {
				writeError(w, "failed to register wallet: wallet already exists", http.StatusConflict)
				return
			}
			writeError(w, "failed to register wallet", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet registered", "address", wallet.Address, "poll_interval", wallet.PollInterval)

		// Return wallet
		resp := walletToResponse(wallet)
		writeJSON(w, resp, http.StatusCreated)
	})
}

// handleUnregisterWallet returns a handler that unregisters a wallet.
// DELETE /api/v1/wallets/{address}
func handleUnregisterWallet(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")

		// Validate address format (basic check)
		if err := validateAddress(address); err != nil {
			logger.Debug("invalid address", "address", address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Check if wallet exists
		exists, err := store.WalletExists(r.Context(), address)
		if err != nil {
			logger.Error("failed to check wallet existence", "address", address, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !exists {
			writeError(w, "wallet not found", http.StatusNotFound)
			return
		}

		// Delete wallet
		if err := store.DeleteWallet(r.Context(), address); err != nil {
			logger.Error("failed to delete wallet", "address", address, "error", err)
			writeError(w, "failed to unregister wallet", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet unregistered", "address", address)
		w.WriteHeader(http.StatusNoContent)
	})
}

// handleGetWallet returns a handler that retrieves a wallet's details.
// GET /api/v1/wallets/{address}
func handleGetWallet(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")

		// Validate address format (basic check)
		if err := validateAddress(address); err != nil {
			logger.Debug("invalid address", "address", address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		wallet, err := store.GetWallet(r.Context(), address)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeError(w, "wallet not found", http.StatusNotFound)
				return
			}
			logger.Error("failed to get wallet", "address", address, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		logger.Debug("wallet retrieved", "address", address)
		resp := walletToResponse(wallet)
		writeJSON(w, resp, http.StatusOK)
	})
}

// handleListWallets returns a handler that lists all registered wallets.
// GET /api/v1/wallets
func handleListWallets(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wallets, err := store.ListWallets(r.Context())
		if err != nil {
			logger.Error("failed to list wallets", "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		logger.Debug("wallets listed", "count", len(wallets))

		// Convert to response format
		resp := make([]walletResponse, len(wallets))
		for i, wallet := range wallets {
			resp[i] = walletToResponse(wallet)
		}

		writeJSON(w, map[string]interface{}{
			"wallets": resp,
		}, http.StatusOK)
	})
}

// handleRegisterWalletWithScheduler returns a handler that registers a new wallet
// and creates a Temporal schedule for polling.
// POST /api/v1/wallets
func handleRegisterWalletWithScheduler(store *db.Store, scheduler temporal.Scheduler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Limit request body size to prevent memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

		var req struct {
			Address      string `json:"address"`
			PollInterval string `json:"poll_interval"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Debug("failed to decode register request", "error", err)
			// Check if error is due to body size limit
			if strings.Contains(err.Error(), "http: request body too large") {
				writeError(w, "request body too large: maximum size is 1MB", http.StatusBadRequest)
				return
			}
			writeError(w, "invalid request body: must be valid JSON", http.StatusBadRequest)
			return
		}

		// Validate address
		if err := validateAddress(req.Address); err != nil {
			logger.Debug("invalid address", "address", req.Address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Parse and validate poll interval
		pollInterval, err := time.ParseDuration(req.PollInterval)
		if err != nil {
			logger.Debug("invalid poll interval", "interval", req.PollInterval, "error", err)
			writeError(w, "invalid poll_interval: must be a valid duration (e.g. '30s', '1m')", http.StatusBadRequest)
			return
		}

		if err := validatePollInterval(pollInterval); err != nil {
			logger.Debug("invalid poll interval value", "interval", pollInterval, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Create wallet in database
		params := db.CreateWalletParams{
			Address:      req.Address,
			PollInterval: pollInterval,
			Status:       "active",
		}

		wallet, err := store.CreateWallet(r.Context(), params)
		if err != nil {
			logger.Error("failed to create wallet", "address", req.Address, "error", err)
			// Check for duplicate key error
			if strings.Contains(err.Error(), "duplicate key") {
				writeError(w, "failed to register wallet: wallet already exists", http.StatusConflict)
				return
			}
			writeError(w, "failed to register wallet", http.StatusInternalServerError)
			return
		}

		// Create Temporal schedule
		if err := scheduler.CreateWalletSchedule(r.Context(), req.Address, pollInterval); err != nil {
			logger.Error("failed to create schedule", "address", req.Address, "error", err)

			// Rollback: delete the wallet we just created
			if delErr := store.DeleteWallet(r.Context(), req.Address); delErr != nil {
				logger.Error("failed to rollback wallet creation", "address", req.Address, "error", delErr)
			}

			writeError(w, "failed to create schedule for wallet", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet registered with schedule", "address", wallet.Address, "poll_interval", wallet.PollInterval)

		// Return wallet
		resp := walletToResponse(wallet)
		writeJSON(w, resp, http.StatusCreated)
	})
}

// handleUnregisterWalletWithScheduler returns a handler that unregisters a wallet
// and deletes its Temporal schedule.
// DELETE /api/v1/wallets/{address}
func handleUnregisterWalletWithScheduler(store *db.Store, scheduler temporal.Scheduler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")

		// Validate address format
		if err := validateAddress(address); err != nil {
			logger.Debug("invalid address", "address", address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Check if wallet exists
		exists, err := store.WalletExists(r.Context(), address)
		if err != nil {
			logger.Error("failed to check wallet existence", "address", address, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !exists {
			writeError(w, "wallet not found", http.StatusNotFound)
			return
		}

		// Delete Temporal schedule first (before DB)
		// If this fails, we don't want to delete the wallet from DB
		if err := scheduler.DeleteWalletSchedule(r.Context(), address); err != nil {
			logger.Error("failed to delete schedule", "address", address, "error", err)
			writeError(w, "failed to delete schedule for wallet", http.StatusInternalServerError)
			return
		}

		// Delete wallet from database
		if err := store.DeleteWallet(r.Context(), address); err != nil {
			logger.Error("failed to delete wallet", "address", address, "error", err)
			// Schedule is already deleted but DB deletion failed
			// This is an inconsistent state, but schedule can be cleaned up by reconciliation
			writeError(w, "failed to unregister wallet", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet unregistered with schedule", "address", address)
		w.WriteHeader(http.StatusNoContent)
	})
}

// walletResponse is the JSON response format for a wallet.
type walletResponse struct {
	Address      string     `json:"address"`
	PollInterval string     `json:"poll_interval"`
	LastPollTime *time.Time `json:"last_poll_time,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// walletToResponse converts a domain Wallet to a response format.
func walletToResponse(w *db.Wallet) walletResponse {
	return walletResponse{
		Address:      w.Address,
		PollInterval: w.PollInterval.String(),
		LastPollTime: w.LastPollTime,
		Status:       w.Status,
		CreatedAt:    w.CreatedAt,
		UpdatedAt:    w.UpdatedAt,
	}
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// validateAddress validates a wallet address for security and format.
func validateAddress(address string) error {
	if address == "" {
		return errorf("address is required")
	}

	if len(address) > maxAddressLength {
		return errorf("address too long: maximum length is %d characters", maxAddressLength)
	}

	// Check for null bytes and control characters
	for _, r := range address {
		if r == 0 || unicode.IsControl(r) {
			return errorf("invalid characters in address: control characters not allowed")
		}
	}

	// Check for common SQL injection patterns
	lowerAddr := strings.ToLower(address)
	sqlPatterns := []string{"drop ", "delete ", "insert ", "update ", "select ", "--", "/*", "*/", ";"}
	for _, pattern := range sqlPatterns {
		if strings.Contains(lowerAddr, pattern) {
			return errorf("invalid characters in address: suspicious pattern detected")
		}
	}

	// Validate against Solana base58 format (optional but recommended)
	// For now we just check alphanumeric with valid base58 chars
	if !validAddressRegex.MatchString(address) {
		return errorf("invalid address format: must contain only valid base58 characters")
	}

	return nil
}

// validatePollInterval validates a poll interval for reasonable bounds.
func validatePollInterval(interval time.Duration) error {
	if interval <= 0 {
		return errorf("poll_interval must be positive")
	}

	if interval < minPollInterval {
		return errorf("poll_interval must be at least %v", minPollInterval)
	}

	if interval > maxPollInterval {
		return errorf("poll_interval cannot exceed %v", maxPollInterval)
	}

	return nil
}

// errorf is a helper to format error strings.
func errorf(format string, args ...interface{}) error {
	return &validationError{msg: strings.TrimSpace(fmt.Sprintf(format, args...))}
}

type validationError struct {
	msg string
}

func (e *validationError) Error() string {
	return e.msg
}

// handleListTransactions returns a handler that lists transactions for a specific wallet.
// GET /api/v1/transactions?wallet_address=ADDRESS&limit=N&offset=N
func handleListTransactions(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		walletAddress := query.Get("wallet_address")

		// wallet_address is required
		if walletAddress == "" {
			writeError(w, "wallet_address query parameter is required", http.StatusBadRequest)
			return
		}

		// Validate address format
		if err := validateAddress(walletAddress); err != nil {
			logger.Debug("invalid address", "address", walletAddress, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Parse limit (default 100, max 1000)
		limit := int32(100)
		if limitStr := query.Get("limit"); limitStr != "" {
			var parsedLimit int
			if _, err := fmt.Sscanf(limitStr, "%d", &parsedLimit); err != nil {
				writeError(w, "invalid limit parameter: must be an integer", http.StatusBadRequest)
				return
			}
			if parsedLimit < 1 {
				writeError(w, "limit must be at least 1", http.StatusBadRequest)
				return
			}
			if parsedLimit > 1000 {
				writeError(w, "limit cannot exceed 1000", http.StatusBadRequest)
				return
			}
			limit = int32(parsedLimit)
		}

		// Parse offset (default 0)
		offset := int32(0)
		if offsetStr := query.Get("offset"); offsetStr != "" {
			var parsedOffset int
			if _, err := fmt.Sscanf(offsetStr, "%d", &parsedOffset); err != nil {
				writeError(w, "invalid offset parameter: must be an integer", http.StatusBadRequest)
				return
			}
			if parsedOffset < 0 {
				writeError(w, "offset cannot be negative", http.StatusBadRequest)
				return
			}
			offset = int32(parsedOffset)
		}

		// Query transactions
		transactions, err := store.ListTransactionsByWallet(r.Context(), db.ListTransactionsByWalletParams{
			WalletAddress: walletAddress,
			Limit:         limit,
			Offset:        offset,
		})
		if err != nil {
			logger.Error("failed to list transactions", "wallet", walletAddress, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		logger.Debug("transactions listed", "wallet", walletAddress, "count", len(transactions))

		// Convert to response format
		resp := make([]transactionResponse, len(transactions))
		for i := range transactions {
			resp[i] = transactionToResponse(transactions[i])
		}

		writeJSON(w, map[string]interface{}{
			"transactions": resp,
			"count":        len(resp),
			"limit":        limit,
			"offset":       offset,
		}, http.StatusOK)
	})
}

// transactionResponse is the JSON response format for a transaction.
type transactionResponse struct {
	Signature          string     `json:"signature"`
	WalletAddress      string     `json:"wallet_address"`
	FromAddress        *string    `json:"from_address,omitempty"`
	Slot               int64      `json:"slot"`
	BlockTime          time.Time  `json:"block_time"`
	Amount             int64      `json:"amount"`
	TokenMint          *string    `json:"token_mint,omitempty"`
	Memo               *string    `json:"memo,omitempty"`
	ConfirmationStatus string     `json:"confirmation_status"`
	CreatedAt          time.Time  `json:"created_at"`
}

// transactionToResponse converts a domain Transaction to a response format.
func transactionToResponse(t *db.Transaction) transactionResponse {
	return transactionResponse{
		Signature:          t.Signature,
		WalletAddress:      t.WalletAddress,
		FromAddress:        t.FromAddress,
		Slot:               t.Slot,
		BlockTime:          t.BlockTime,
		Amount:             t.Amount,
		TokenMint:          t.TokenMint,
		Memo:               t.Memo,
		ConfirmationStatus: t.ConfirmationStatus,
		CreatedAt:          t.CreatedAt,
	}
}
