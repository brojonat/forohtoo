package solana

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// RPCClient is an interface for the Solana RPC operations we need.
// This allows us to mock the RPC layer in tests without hitting real Solana nodes.
type RPCClient interface {
	GetSignaturesForAddress(
		ctx context.Context,
		address solana.PublicKey,
		opts *rpc.GetSignaturesForAddressOpts,
	) ([]*rpc.TransactionSignature, error)

	GetTransaction(
		ctx context.Context,
		signature solana.Signature,
		opts *rpc.GetTransactionOpts,
	) (*rpc.GetTransactionResult, error)
}

// Client provides methods for polling Solana transactions.
// It wraps the RPC client with domain-specific operations.
type Client struct {
	rpc    RPCClient
	logger *slog.Logger
}

// NewClient creates a new Solana client.
func NewClient(rpcClient RPCClient, logger *slog.Logger) *Client {
	return &Client{
		rpc:    rpcClient,
		logger: logger,
	}
}

// GetTransactionsSinceParams contains parameters for fetching transactions.
type GetTransactionsSinceParams struct {
	Wallet             solana.PublicKey
	LastSignature      *solana.Signature
	Limit              int
	ExistingSignatures []string
}

// GetTransactionsSince polls for new transactions after the given signature.
// If lastSignature is nil, it returns the most recent transactions.
// Returns transactions in descending order (newest first).
//
// This method fetches both signature metadata and full transaction details,
// parsing amounts, token mints, and memos from the transaction instructions.
func (c *Client) GetTransactionsSince(
	ctx context.Context,
	params GetTransactionsSinceParams,
) ([]*Transaction, error) {
	// Build RPC options
	opts := &rpc.GetSignaturesForAddressOpts{
		Limit: &params.Limit,
	}
	if params.LastSignature != nil {
		opts.Until = *params.LastSignature
	}

	// Fetch signatures from RPC
	signatures, err := c.rpc.GetSignaturesForAddress(ctx, params.Wallet, opts)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to get signatures",
			"wallet", params.Wallet.String(),
			"error", err,
		)
		return nil, err
	}

	c.logger.DebugContext(ctx, "fetched transaction signatures",
		"wallet", params.Wallet.String(),
		"count", len(signatures),
	)

	// Create a lookup map for existing signatures to avoid reprocessing.
	existingSigs := make(map[string]struct{})
	for _, sig := range params.ExistingSignatures {
		existingSigs[sig] = struct{}{}
	}

	// Fetch and parse full transaction details for each signature
	transactions := make([]*Transaction, 0, len(signatures))
	for _, sig := range signatures {

		// Skip if we have already processed this transaction.
		if _, exists := existingSigs[sig.Signature.String()]; exists {
			c.logger.DebugContext(ctx, "skipping already processed transaction",
				"signature", sig.Signature.String(),
			)
			continue
		}

		// Add a small delay to respect RPC rate limits (Helius: ~10 RPS)
		time.Sleep(100 * time.Millisecond)

		var result *rpc.GetTransactionResult
		var err error

		// Retry logic for fetching transaction details
		for attempt := range 10 {
			// Fetch full transaction details with support for versioned transactions
			txnOpts := &rpc.GetTransactionOpts{
				Encoding:                       solana.EncodingBase64,
				MaxSupportedTransactionVersion: &[]uint64{0}[0],
			}
			result, err = c.rpc.GetTransaction(ctx, sig.Signature, txnOpts)
			if err == nil {
				break // Success
			}

			// Handle rate limiting (429 Too Many Requests)
			if strings.Contains(err.Error(), "429") {
				c.logger.WarnContext(ctx, "rate limited, sleeping before retry",
					"signature", sig.Signature.String(),
					"attempt", attempt+1,
				)
				time.Sleep(5 * time.Second)
				continue // Sleep and try again
			}

			// Handle parsing errors for legacy transactions
			if strings.Contains(err.Error(), "expects '\"' or 'n', but found '{'") {
				c.logger.WarnContext(ctx, "could not parse as versioned tx, retrying as legacy",
					"signature", sig.Signature.String(),
				)

				// Retry immediately without version support
				legacyTxnOpts := &rpc.GetTransactionOpts{
					Encoding: solana.EncodingBase64,
				}
				result, err = c.rpc.GetTransaction(ctx, sig.Signature, legacyTxnOpts)
				if err == nil {
					break // Success on fallback
				}
			}

			c.logger.WarnContext(ctx, "failed to get transaction on attempt",
				"signature", sig.Signature.String(),
				"attempt", attempt+1,
				"error", err,
			)
			// Sleep before retrying to avoid hammering the endpoint
			time.Sleep(2 * time.Second)
		}

		if err != nil {
			// Log warning but continue with other transactions
			// Transaction might be pruned or not available after retries
			c.logger.WarnContext(ctx, "failed to get transaction details after retries, using metadata only",
				"signature", sig.Signature.String(),
				"error", err,
			)
			// Fall back to metadata-only transaction
			transactions = append(transactions, signatureToDomain(sig))
			continue
		}

		// Parse transaction to extract amount, token mint, and memo
		txn, err := parseTransactionFromResult(sig, result)
		if err != nil {
			// Log warning but continue with other transactions
			c.logger.WarnContext(ctx, "failed to parse transaction, using metadata only",
				"signature", sig.Signature.String(),
				"error", err,
			)
			// Fall back to metadata-only transaction
			transactions = append(transactions, signatureToDomain(sig))
			continue
		}

		transactions = append(transactions, txn)
	}

	c.logger.InfoContext(ctx, "fetched and parsed transactions",
		"wallet", params.Wallet.String(),
		"count", len(transactions),
	)

	return transactions, nil
}

// GetTransaction fetches and parses a specific transaction by signature.
// func (c *Client) GetTransaction(
// 	ctx context.Context,
// 	signature solana.Signature,
// ) (*Transaction, error) {
// 	// TODO: implement
// 	return nil, nil
// }
