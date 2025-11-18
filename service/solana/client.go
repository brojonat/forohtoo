package solana

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/brojonat/forohtoo/service/metrics"
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
	rpc      RPCClient
	logger   *slog.Logger
	metrics  *metrics.Metrics
	endpoint string // RPC endpoint identifier for metrics (e.g., "mainnet", "devnet", rpc host)
}

// NewClient creates a new Solana client.
// The endpoint parameter is used for metrics labeling (e.g., "mainnet", "devnet", or RPC hostname).
// If metrics is nil, no metrics will be recorded.
func NewClient(rpcClient RPCClient, endpoint string, m *metrics.Metrics, logger *slog.Logger) *Client {
	return &Client{
		rpc:      rpcClient,
		logger:   logger,
		metrics:  m,
		endpoint: endpoint,
	}
}

// GetTransactionsSinceParams contains parameters for fetching transactions.
type GetTransactionsSinceParams struct {
	Wallet             solana.PublicKey
	Network            string // "mainnet" or "devnet"
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

	// Log RPC call parameters for debugging
	c.logger.DebugContext(ctx, "calling GetSignaturesForAddress",
		"wallet", params.Wallet.String(),
		"limit", params.Limit,
		"until", params.LastSignature,
		"existing_sigs_count", len(params.ExistingSignatures),
	)

	// Fetch signatures from RPC
	start := time.Now()
	signatures, err := c.rpc.GetSignaturesForAddress(ctx, params.Wallet, opts)
	duration := time.Since(start).Seconds()

	// Record metrics for GetSignaturesForAddress call
	status := "success"
	if err != nil {
		status = "error"
		c.logger.ErrorContext(ctx, "failed to get signatures",
			"wallet", params.Wallet.String(),
			"error", err,
		)
	}
	if c.metrics != nil {
		c.metrics.RecordRPCCall("GetSignaturesForAddress", status, c.endpoint, duration)
		if err == nil {
			c.metrics.RecordRPCSignaturesPerCall(c.endpoint, float64(len(signatures)))
		}
	}

	if err != nil {
		return nil, err
	}

	c.logger.DebugContext(ctx, "fetched transaction signatures",
		"wallet", params.Wallet.String(),
		"count", len(signatures),
	)

	// Log first few signatures for debugging
	if len(signatures) > 0 {
		firstSigs := make([]string, 0, min(3, len(signatures)))
		for i := 0; i < min(3, len(signatures)); i++ {
			firstSigs = append(firstSigs, signatures[i].Signature.String()[:20]+"...")
		}
		c.logger.DebugContext(ctx, "RPC returned signatures",
			"first_signatures", firstSigs,
			"total_count", len(signatures),
		)
	} else {
		c.logger.DebugContext(ctx, "RPC returned ZERO signatures - investigating",
			"wallet", params.Wallet.String(),
			"limit", params.Limit,
			"until", params.LastSignature,
		)
	}

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
			// Record that we skipped this transaction (deduplication working)
			if c.metrics != nil {
				c.metrics.RecordTransactionsSkipped(params.Wallet.String(), "already_fetched", 1)
			}
			continue
		}

		// Add a delay to respect RPC rate limits
		// Public mainnet: very conservative (1-2 RPS max)
		// Helius/Premium: can be reduced to 100-150ms
		time.Sleep(600 * time.Millisecond)

		var result *rpc.GetTransactionResult
		var err error

		// Retry logic with exponential backoff
		// Public RPC: 3 attempts max to avoid long delays
		// Premium RPC: can increase to 5
		const maxAttempts = 3
		for attempt := range maxAttempts {
			// Fetch full transaction details with support for versioned transactions
			txnOpts := &rpc.GetTransactionOpts{
				Encoding:                       solana.EncodingBase64,
				MaxSupportedTransactionVersion: &[]uint64{0}[0],
			}
			txnStart := time.Now()
			result, err = c.rpc.GetTransaction(ctx, sig.Signature, txnOpts)
			txnDuration := time.Since(txnStart).Seconds()

			// Record metrics for GetTransaction call
			txnStatus := "success"
			if err != nil {
				txnStatus = "error"
			}
			if c.metrics != nil {
				c.metrics.RecordRPCCall("GetTransaction", txnStatus, c.endpoint, txnDuration)
			}

			if err == nil {
				break // Success
			}

			// Handle rate limiting (429 Too Many Requests) with longer backoff
			if strings.Contains(err.Error(), "429") {
				backoff := time.Duration(2<<uint(attempt)) * time.Second // 2s, 4s, 8s, 16s, 32s
				c.logger.WarnContext(ctx, "rate limited, sleeping before retry",
					"signature", sig.Signature.String(),
					"attempt", attempt+1,
					"backoff_seconds", backoff.Seconds(),
				)
				// Record rate limit hit
				if c.metrics != nil {
					c.metrics.RecordRateLimitHit(c.endpoint)
					c.metrics.RecordRPCRetry("GetTransaction", "rate_limit")
				}
				time.Sleep(backoff)
				continue // Sleep and try again
			}

			// Handle parsing errors for legacy transactions
			if strings.Contains(err.Error(), "expects '\"' or 'n', but found '{'") {
				c.logger.WarnContext(ctx, "could not parse as versioned tx, retrying as legacy",
					"signature", sig.Signature.String(),
				)

				// Record retry for parse error
				if c.metrics != nil {
					c.metrics.RecordRPCRetry("GetTransaction", "parse_error")
				}

				// Retry immediately without version support
				legacyTxnOpts := &rpc.GetTransactionOpts{
					Encoding: solana.EncodingBase64,
				}
				legacyStart := time.Now()
				result, err = c.rpc.GetTransaction(ctx, sig.Signature, legacyTxnOpts)
				legacyDuration := time.Since(legacyStart).Seconds()

				// Record metrics for legacy retry
				legacyStatus := "success"
				if err != nil {
					legacyStatus = "error"
				}
				if c.metrics != nil {
					c.metrics.RecordRPCCall("GetTransaction", legacyStatus, c.endpoint, legacyDuration)
				}

				if err == nil {
					break // Success on fallback
				}
			}

			// Exponential backoff for other errors (timeout, network, etc.)
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s, 8s, 16s
			c.logger.WarnContext(ctx, "failed to get transaction on attempt",
				"signature", sig.Signature.String(),
				"attempt", attempt+1,
				"error", err,
				"backoff_seconds", backoff.Seconds(),
			)
			// Record retry
			if c.metrics != nil {
				c.metrics.RecordRPCRetry("GetTransaction", "timeout_or_error")
			}
			time.Sleep(backoff)
		}

		if err != nil {
			// Log warning but continue with other transactions
			// Transaction might be pruned or not available after retries
			c.logger.WarnContext(ctx, "failed to get transaction details after retries, using metadata only",
				"signature", sig.Signature.String(),
				"error", err,
			)
			// Fall back to metadata-only transaction
			txn := signatureToDomain(sig)
			txn.Network = params.Network
			transactions = append(transactions, txn)
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
			// Record parse failure
			if c.metrics != nil {
				c.metrics.RecordTransactionParsed(params.Wallet.String(), "error")
			}
			// Fall back to metadata-only transaction
			fallbackTxn := signatureToDomain(sig)
			fallbackTxn.Network = params.Network
			transactions = append(transactions, fallbackTxn)
			continue
		}

		// Set network on the parsed transaction
		txn.Network = params.Network

		// Record successful parse
		if c.metrics != nil {
			c.metrics.RecordTransactionParsed(params.Wallet.String(), "success")
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
