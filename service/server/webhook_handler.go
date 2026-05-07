package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/helius"
	natspkg "github.com/brojonat/forohtoo/service/nats"
)

// handleHeliusWebhook returns a handler that receives enhanced transaction events
// from Helius webhooks, writes them to the database, and publishes to NATS.
//
// This replaces the Temporal polling workflow for transaction ingestion when
// Helius webhooks are configured.
//
// POST /api/v1/webhooks/helius
func handleHeliusWebhook(
	store *db.Store,
	publisher natspkg.Publisher,
	authToken string,
	logger *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate auth header from Helius
		authHeader := r.Header.Get("Authorization")
		if authHeader != authToken {
			logger.Warn("webhook auth failed",
				"remote_addr", r.RemoteAddr,
				"auth_header_present", authHeader != "",
			)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Read body
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20)) // 10MB max
		if err != nil {
			logger.Error("failed to read webhook body", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse enhanced transactions
		txns, err := helius.ParseWebhookPayload(body)
		if err != nil {
			logger.Error("failed to parse webhook payload", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(txns) == 0 {
			w.WriteHeader(http.StatusOK)
			return
		}

		logger.Debug("received Helius webhook",
			"transaction_count", len(txns),
			"first_signature", txns[0].Signature,
		)

		// Build address lookup map from registered wallets
		addressMap, err := buildAddressMap(r.Context(), store)
		if err != nil {
			logger.Error("failed to build address map", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Parse transactions and match against registered wallets
		params := helius.ParseEnhancedTransactions(txns, addressMap, logger)

		if len(params) == 0 {
			logger.Debug("no transactions matched registered wallets",
				"transaction_count", len(txns),
			)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Write matched transactions to database and publish to NATS
		written := 0
		skipped := 0
		var writtenTxns []*db.Transaction

		for _, p := range params {
			dbTxn, err := store.CreateTransaction(r.Context(), p)
			if err != nil {
				if isDuplicateError(err) {
					skipped++
					continue
				}
				logger.Error("failed to write transaction",
					"signature", p.Signature,
					"error", err,
				)
				continue
			}
			written++
			writtenTxns = append(writtenTxns, dbTxn)
		}

		// Publish to NATS for SSE subscribers
		if len(writtenTxns) > 0 && publisher != nil {
			events := make([]*natspkg.TransactionEvent, 0, len(writtenTxns))
			for _, txn := range writtenTxns {
				events = append(events, natspkg.FromDBTransaction(txn))
			}

			if err := publisher.PublishTransactionBatch(r.Context(), events); err != nil {
				logger.Error("failed to publish transactions to NATS",
					"count", len(events),
					"error", err,
				)
			} else {
				logger.Debug("published webhook transactions to NATS",
					"count", len(events),
				)
			}
		}

		logger.Info("processed Helius webhook",
			"received", len(txns),
			"matched", len(params),
			"written", written,
			"skipped", skipped,
		)

		w.WriteHeader(http.StatusOK)
	})
}

// buildAddressMap creates a lookup from monitored addresses to wallet info
// by querying all active wallets from the database.
//
// For SOL assets, the key is the wallet address itself.
// For SPL token assets, the key is the associated token address (ATA).
func buildAddressMap(ctx context.Context, store *db.Store) (map[string]helius.WalletLookup, error) {
	if store == nil {
		return nil, fmt.Errorf("store is nil")
	}

	wallets, err := store.ListActiveWallets(ctx)
	if err != nil {
		return nil, err
	}

	addressMap := make(map[string]helius.WalletLookup, len(wallets))
	for _, w := range wallets {
		lookup := helius.WalletLookup{
			WalletAddress: w.Address,
			Network:       w.Network,
			AssetType:     w.AssetType,
			TokenMint:     w.TokenMint,
		}

		if w.AssetType == "sol" {
			// For SOL, monitor the wallet address directly
			addressMap[w.Address] = lookup
		} else if w.AssociatedTokenAddress != nil {
			// For SPL tokens, monitor the ATA
			addressMap[*w.AssociatedTokenAddress] = lookup
		}
	}

	return addressMap, nil
}

// isDuplicateError checks if an error is a duplicate key constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key value violates unique constraint") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "already exists")
}
