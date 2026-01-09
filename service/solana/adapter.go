package solana

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

// SelectRandomEndpoint picks a random endpoint from the pool.
// Returns error if endpoints slice is empty.
// Uses Go 1.20+ automatic random seeding - no manual seeding needed.
func SelectRandomEndpoint(endpoints []string) (string, error) {
	if len(endpoints) == 0 {
		return "", fmt.Errorf("no RPC endpoints configured")
	}
	return endpoints[rand.Intn(len(endpoints))], nil
}

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
//
// Configures HTTP client with 60-second timeout to handle slow RPC responses.
func NewRPCClient(rpcURL string) RPCClient {
	// Create HTTP client with longer timeout
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
	}

	// Create JSON-RPC client with custom HTTP client
	jsonrpcClient := jsonrpc.NewClientWithOpts(rpcURL, &jsonrpc.RPCClientOpts{
		HTTPClient: httpClient,
	})

	return &realRPCClient{
		client: rpc.NewWithCustomRPCClient(jsonrpcClient),
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
