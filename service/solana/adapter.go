package solana

import (
	"context"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// realRPCClient adapts the actual solana-go RPC client to our RPCClient interface.
// This adapter allows us to control the interface and makes testing easier.
type realRPCClient struct {
	client *rpc.Client
}

// NewRPCClient creates a new RPCClient that wraps the solana-go RPC client.
// For premium RPC endpoints that require API keys, include the key in the URL:
// - Helius: https://mainnet.helius-rpc.com/?api-key=YOUR-KEY
// - QuickNode: https://YOUR-ENDPOINT.quiknode.pro/YOUR-KEY/
// - Alchemy: https://solana-mainnet.g.alchemy.com/v2/YOUR-KEY
func NewRPCClient(rpcURL string) RPCClient {
	return &realRPCClient{
		client: rpc.New(rpcURL),
	}
}

func (r *realRPCClient) GetSignaturesForAddress(
	ctx context.Context,
	address solana.PublicKey,
	opts *rpc.GetSignaturesForAddressOpts,
) ([]*rpc.TransactionSignature, error) {
	// The real client's method signature matches ours, so we can call it directly
	out, err := r.client.GetSignaturesForAddressWithOpts(ctx, address, opts)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *realRPCClient) GetTransaction(
	ctx context.Context,
	signature solana.Signature,
	opts *rpc.GetTransactionOpts,
) (*rpc.GetTransactionResult, error) {
	return r.client.GetTransaction(ctx, signature, opts)
}
