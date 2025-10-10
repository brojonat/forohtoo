package solana

import (
	"context"
	"log/slog"

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

// GetTransactionsSince polls for new transactions after the given signature.
// If lastSignature is nil, it returns the most recent transactions.
// Returns transactions in descending order (newest first).
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

	// Convert RPC signatures to domain transactions
	transactions := make([]*Transaction, 0, len(signatures))
	for _, sig := range signatures {
		txn := signatureToDomain(sig)
		transactions = append(transactions, txn)
	}

	c.logger.DebugContext(ctx, "fetched transactions",
		"wallet", params.Wallet.String(),
		"count", len(transactions),
	)

	return transactions, nil
}

// GetTransaction fetches and parses a specific transaction by signature.
func (c *Client) GetTransaction(
	ctx context.Context,
	signature solana.Signature,
) (*Transaction, error) {
	// TODO: implement
	return nil, nil
}
