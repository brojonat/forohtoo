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

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	solanago "github.com/gagliardetto/solana-go"
)

const (
	maxRequestBodySize = 1 << 20 // 1MB - plenty for wallet registration
	maxAddressLength   = 100     // Solana addresses are 44 chars, give buffer
	minPollInterval    = 60 * time.Second
	maxPollInterval    = 24 * time.Hour
)

var (
	// Valid Solana address characters: base58 (no 0, O, I, l)
	validAddressRegex = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]+$`)
)

// handleGetWalletAsset returns a handler that retrieves all assets for a wallet address.
// GET /api/v1/wallet-assets/{address}?network={network}
// Returns all registered assets for the given wallet address and network.
func handleGetWalletAsset(store *db.Store, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")
		network := r.URL.Query().Get("network")

		// Validate address format
		if err := validateAddress(address); err != nil {
			logger.Debug("invalid address", "address", address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate network
		if err := validateNetwork(network); err != nil {
			logger.Debug("invalid network", "network", network, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Get all assets for this wallet + network
		assets, err := store.ListWalletAssets(r.Context(), address, network)
		if err != nil {
			logger.Error("failed to get wallet assets", "address", address, "network", network, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if len(assets) == 0 {
			writeError(w, "wallet not found", http.StatusNotFound)
			return
		}

		logger.Debug("wallet assets retrieved", "address", address, "network", network, "count", len(assets))

		// Convert to response format
		resp := make([]walletResponse, len(assets))
		for i, asset := range assets {
			resp[i] = walletToResponse(asset)
		}

		writeJSON(w, map[string]interface{}{
			"address": address,
			"network": network,
			"assets":  resp,
		}, http.StatusOK)
	})
}

// handleListWalletAssets returns a handler that lists all registered wallet assets.
// GET /api/v1/wallet-assets
func handleListWalletAssets(store *db.Store, logger *slog.Logger) http.Handler {
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

// handleRegisterWalletAsset returns a handler that registers a new wallet+asset
// and creates a Temporal schedule for polling.
// With payment gateway enabled, new wallets require payment first.
// POST /api/v1/wallet-assets
func handleRegisterWalletAsset(store *db.Store, scheduler temporal.Scheduler, temporalClient *temporal.Client, cfg *config.Config, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Limit request body size to prevent memory exhaustion
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

		var req struct {
			Address      string `json:"address"`
			Network      string `json:"network"` // "mainnet" or "devnet"
			Asset        struct {
				Type      string `json:"type"`       // "sol" or "spl-token"
				TokenMint string `json:"token_mint"` // required when type == "spl-token"
			} `json:"asset"`
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

		// Validate network
		if err := validateNetwork(req.Network); err != nil {
			logger.Debug("invalid network", "network", req.Network, "error", err)
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

		// Validate asset type
		if err := validateAssetType(req.Asset.Type); err != nil {
			logger.Debug("invalid asset type", "type", req.Asset.Type, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate and process asset-specific fields
		var tokenMint string
		var ata *string

		if req.Asset.Type == "sol" {
			// For SOL, mint should be empty
			tokenMint = ""
			ata = nil
		} else if req.Asset.Type == "spl-token" {
			// For SPL tokens, mint is required
			if req.Asset.TokenMint == "" {
				writeError(w, "token_mint is required for spl-token asset type", http.StatusBadRequest)
				return
			}

			// Validate mint address format
			if err := validateTokenMint(req.Asset.TokenMint); err != nil {
				logger.Debug("invalid token mint", "mint", req.Asset.TokenMint, "error", err)
				writeError(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Verify mint is supported for this network
			if !cfg.IsMintSupported(req.Network, req.Asset.TokenMint) {
				supportedMints, _ := cfg.GetSupportedMints(req.Network)
				writeError(w, fmt.Sprintf("unsupported token mint for %s: supported mints are %v", req.Network, supportedMints), http.StatusBadRequest)
				return
			}

			tokenMint = req.Asset.TokenMint

			// Compute ATA
			ataAddr, err := computeAssociatedTokenAddress(req.Address, tokenMint)
			if err != nil {
				logger.Error("failed to compute ATA", "address", req.Address, "mint", tokenMint, "error", err)
				writeError(w, "failed to compute associated token address", http.StatusInternalServerError)
				return
			}
			ata = &ataAddr
		}

		// Check if wallet exists (for payment gateway)
		walletExists, err := store.WalletExists(r.Context(), req.Address, req.Network, req.Asset.Type, tokenMint)
		if err != nil {
			logger.Error("failed to check wallet existence", "address", req.Address, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// If wallet doesn't exist and payment gateway is enabled, require payment
		if !walletExists && cfg.PaymentGateway.Enabled {
			logger.Debug("new wallet registration with payment gateway enabled",
				"address", req.Address,
				"network", req.Network,
				"asset_type", req.Asset.Type,
			)

			// Determine USDC mint based on service network
			var usdcMint string
			if cfg.PaymentGateway.ServiceNetwork == "mainnet" {
				usdcMint = cfg.USDCMainnetMintAddress
			} else {
				usdcMint = cfg.USDCDevnetMintAddress
			}

			// Generate payment invoice (always in USDC)
			invoice := generatePaymentInvoice(&cfg.PaymentGateway, usdcMint)

			// Start Temporal workflow for payment-gated registration
			workflowID := fmt.Sprintf("payment-registration:%s", invoice.ID)
			workflowInput := temporal.PaymentGatedRegistrationInput{
				Address:                req.Address,
				Network:                req.Network,
				AssetType:              req.Asset.Type,
				TokenMint:              tokenMint,
				AssociatedTokenAddress: ata,
				PollInterval:           pollInterval,
				ServiceWallet:          cfg.PaymentGateway.ServiceWallet,
				ServiceNetwork:         cfg.PaymentGateway.ServiceNetwork,
				FeeAmount:              cfg.PaymentGateway.FeeAmount,
				PaymentMemo:            invoice.Memo,
				PaymentTimeout:         cfg.PaymentGateway.PaymentTimeout,
			}

			workflowOptions := temporal.StartWorkflowOptions{
				ID:        workflowID,
				TaskQueue: cfg.TemporalTaskQueue,
			}

			err = temporalClient.StartWorkflow(r.Context(), workflowOptions, "PaymentGatedRegistrationWorkflow", workflowInput)
			if err != nil {
				logger.Error("failed to start payment workflow", "error", err, "workflow_id", workflowID)
				writeError(w, "failed to start payment workflow", http.StatusInternalServerError)
				return
			}

			logger.Info("payment workflow started",
				"workflow_id", workflowID,
				"invoice_id", invoice.ID,
				"address", req.Address,
			)

			// Return 402 Payment Required with invoice and workflow ID
			response := map[string]interface{}{
				"status":      "payment_required",
				"invoice":     invoice,
				"workflow_id": workflowID,
				"status_url":  fmt.Sprintf("/api/v1/registration-status/%s", workflowID),
			}
			writeJSON(w, response, http.StatusPaymentRequired)
			return
		}

		// Wallet exists or payment gateway disabled - proceed with normal upsert
		// Upsert wallet+asset in database (create or update if exists)
		params := db.UpsertWalletParams{
			Address:                req.Address,
			Network:                req.Network,
			AssetType:              req.Asset.Type,
			TokenMint:              tokenMint,
			AssociatedTokenAddress: ata,
			PollInterval:           pollInterval,
			Status:                 "active",
		}

		wallet, err := store.UpsertWallet(r.Context(), params)
		if err != nil {
			logger.Error("failed to upsert wallet asset", "address", req.Address, "error", err)
			writeError(w, "failed to register wallet asset", http.StatusInternalServerError)
			return
		}

		// Upsert Temporal schedule (create or update if exists)
		if err := scheduler.UpsertWalletAssetSchedule(r.Context(), req.Address, req.Network, req.Asset.Type, tokenMint, ata, pollInterval); err != nil {
			logger.Error("failed to upsert schedule", "address", req.Address, "network", req.Network, "error", err)

			// Rollback: delete the wallet asset we just created/updated
			if delErr := store.DeleteWallet(r.Context(), req.Address, req.Network, req.Asset.Type, tokenMint); delErr != nil {
				logger.Error("failed to rollback wallet asset upsert", "address", req.Address, "network", req.Network, "error", delErr)
			}

			writeError(w, "failed to upsert schedule for wallet asset", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet asset upserted with schedule",
			"address", wallet.Address,
			"network", req.Network,
			"asset_type", req.Asset.Type,
			"token_mint", tokenMint,
			"poll_interval", wallet.PollInterval,
		)

		// Return wallet asset
		resp := walletToResponse(wallet)
		writeJSON(w, resp, http.StatusCreated)
	})
}

// handleUnregisterWalletAsset returns a handler that unregisters a wallet+asset
// and deletes its Temporal schedule.
// DELETE /api/v1/wallet-assets/{address}?network={network}&asset_type={type}&token_mint={mint}
func handleUnregisterWalletAsset(store *db.Store, scheduler temporal.Scheduler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := r.PathValue("address")
		network := r.URL.Query().Get("network")
		assetType := r.URL.Query().Get("asset_type")
		tokenMint := r.URL.Query().Get("token_mint")

		// Validate address format
		if err := validateAddress(address); err != nil {
			logger.Debug("invalid address", "address", address, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate network
		if err := validateNetwork(network); err != nil {
			logger.Debug("invalid network", "network", network, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate asset type
		if err := validateAssetType(assetType); err != nil {
			logger.Debug("invalid asset type", "type", assetType, "error", err)
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Normalize token mint (empty for SOL)
		if assetType == "sol" {
			tokenMint = ""
		}

		// Check if wallet asset exists
		exists, err := store.WalletExists(r.Context(), address, network, assetType, tokenMint)
		if err != nil {
			logger.Error("failed to check wallet asset existence", "address", address, "network", network, "error", err)
			writeError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !exists {
			writeError(w, "wallet asset not found", http.StatusNotFound)
			return
		}

		// Delete Temporal schedule first (before DB)
		// If this fails, we don't want to delete the wallet asset from DB
		if err := scheduler.DeleteWalletAssetSchedule(r.Context(), address, network, assetType, tokenMint); err != nil {
			logger.Error("failed to delete schedule", "address", address, "network", network, "error", err)
			writeError(w, "failed to delete schedule for wallet asset", http.StatusInternalServerError)
			return
		}

		// Delete wallet asset from database
		if err := store.DeleteWallet(r.Context(), address, network, assetType, tokenMint); err != nil {
			logger.Error("failed to delete wallet asset", "address", address, "network", network, "error", err)
			// Schedule is already deleted but DB deletion failed
			// This is an inconsistent state, but schedule can be cleaned up by reconciliation
			writeError(w, "failed to unregister wallet asset", http.StatusInternalServerError)
			return
		}

		logger.Info("wallet asset unregistered with schedule",
			"address", address,
			"network", network,
			"asset_type", assetType,
			"token_mint", tokenMint,
		)
		w.WriteHeader(http.StatusNoContent)
	})
}

// handleGetRegistrationStatus returns a handler that checks the status of a payment-gated registration workflow.
// GET /api/v1/registration-status/{workflow_id}
func handleGetRegistrationStatus(temporalClient *temporal.Client, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workflowID := r.PathValue("workflow_id")

		if workflowID == "" {
			writeError(w, "workflow_id is required", http.StatusBadRequest)
			return
		}

		// Query workflow execution
		describeResp, err := temporalClient.DescribeWorkflow(r.Context(), workflowID)
		if err != nil {
			logger.Debug("workflow not found", "workflow_id", workflowID, "error", err)
			writeError(w, "workflow not found", http.StatusNotFound)
			return
		}

		// Check if workflow is still running
		if describeResp.IsRunning {
			logger.Debug("workflow still running", "workflow_id", workflowID)
			writeJSON(w, map[string]interface{}{
				"workflow_id": workflowID,
				"status":      "pending",
				"state":       describeResp.Status,
			}, http.StatusOK)
			return
		}

		// Workflow completed - get result
		result, err := temporalClient.GetWorkflowResult(r.Context(), workflowID)
		if err != nil {
			logger.Error("failed to get workflow result", "workflow_id", workflowID, "error", err)

			// Workflow may have failed
			writeJSON(w, map[string]interface{}{
				"workflow_id": workflowID,
				"status":      "failed",
				"error":       err.Error(),
			}, http.StatusOK)
			return
		}

		// Parse result as PaymentGatedRegistrationResult
		var wfResult temporal.PaymentGatedRegistrationResult
		if err := result.Get(&wfResult); err != nil {
			logger.Error("failed to parse workflow result", "workflow_id", workflowID, "error", err)
			writeJSON(w, map[string]interface{}{
				"workflow_id": workflowID,
				"status":      "failed",
				"error":       "failed to parse workflow result",
			}, http.StatusOK)
			return
		}

		// Return workflow result
		response := map[string]interface{}{
			"workflow_id":        workflowID,
			"status":             wfResult.Status,
			"address":            wfResult.Address,
			"network":            wfResult.Network,
			"asset_type":         wfResult.AssetType,
			"token_mint":         wfResult.TokenMint,
			"payment_amount":     wfResult.PaymentAmount,
		}

		if wfResult.PaymentSignature != nil {
			response["payment_signature"] = *wfResult.PaymentSignature
		}
		if !wfResult.RegisteredAt.IsZero() {
			response["registered_at"] = wfResult.RegisteredAt
		}
		if wfResult.Error != nil {
			response["error"] = *wfResult.Error
		}

		writeJSON(w, response, http.StatusOK)
	})
}

// walletResponse is the JSON response format for a wallet asset.
type walletResponse struct {
	Address                string     `json:"address"`
	Network                string     `json:"network"`
	AssetType              string     `json:"asset_type"` // "sol" or "spl-token"
	TokenMint              string     `json:"token_mint"` // empty for SOL, mint address for SPL tokens
	AssociatedTokenAddress *string    `json:"associated_token_address,omitempty"`
	PollInterval           string     `json:"poll_interval"`
	LastPollTime           *time.Time `json:"last_poll_time,omitempty"`
	Status                 string     `json:"status"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// walletToResponse converts a domain Wallet to a response format.
func walletToResponse(w *db.Wallet) walletResponse {
	return walletResponse{
		Address:                w.Address,
		Network:                w.Network,
		AssetType:              w.AssetType,
		TokenMint:              w.TokenMint,
		AssociatedTokenAddress: w.AssociatedTokenAddress,
		PollInterval:           w.PollInterval.String(),
		LastPollTime:           w.LastPollTime,
		Status:                 w.Status,
		CreatedAt:              w.CreatedAt,
		UpdatedAt:              w.UpdatedAt,
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

// validateNetwork validates a network parameter.
func validateNetwork(network string) error {
	if network == "" {
		return errorf("network is required")
	}

	if network != "mainnet" && network != "devnet" {
		return errorf("invalid network: must be 'mainnet' or 'devnet'")
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

// validateAssetType validates an asset type parameter.
func validateAssetType(assetType string) error {
	if assetType == "" {
		return errorf("asset_type is required")
	}

	if assetType != "sol" && assetType != "spl-token" {
		return errorf("invalid asset_type: must be 'sol' or 'spl-token'")
	}

	return nil
}

// validateTokenMint validates a token mint address.
func validateTokenMint(mint string) error {
	// For SOL, mint should be empty
	if mint == "" {
		return nil
	}

	// For SPL tokens, validate the mint address format
	if err := validateAddress(mint); err != nil {
		return errorf("invalid token_mint: %v", err)
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

// computeAssociatedTokenAddress computes the ATA for a wallet address and token mint.
// Returns the ATA address as a string, or an error if the computation fails.
func computeAssociatedTokenAddress(walletAddress string, tokenMint string) (string, error) {
	wallet, err := solanago.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return "", fmt.Errorf("invalid wallet address: %w", err)
	}

	mint, err := solanago.PublicKeyFromBase58(tokenMint)
	if err != nil {
		return "", fmt.Errorf("invalid token mint: %w", err)
	}

	ata, _, err := solanago.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return "", fmt.Errorf("failed to compute ATA: %w", err)
	}

	return ata.String(), nil
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
	Signature          string    `json:"signature"`
	WalletAddress      string    `json:"wallet_address"`
	FromAddress        *string   `json:"from_address,omitempty"`
	Slot               int64     `json:"slot"`
	BlockTime          time.Time `json:"block_time"`
	Amount             int64     `json:"amount"`
	TokenType          *string   `json:"token_type,omitempty"`
	Memo               *string   `json:"memo,omitempty"`
	ConfirmationStatus string    `json:"confirmation_status"`
	CreatedAt          time.Time `json:"created_at"`
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
		TokenType:          t.TokenMint,
		Memo:               t.Memo,
		ConfirmationStatus: t.ConfirmationStatus,
		CreatedAt:          t.CreatedAt,
	}
}
