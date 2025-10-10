package solana

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRPCClient implements RPCClient for testing.
// It's behavior-focused: we set what it should return, not verify call sequences.
type mockRPCClient struct {
	signatures   []*rpc.TransactionSignature
	transactions map[string]*rpc.GetTransactionResult
	err          error
}

func (m *mockRPCClient) GetSignaturesForAddress(
	ctx context.Context,
	address solana.PublicKey,
	opts *rpc.GetSignaturesForAddressOpts,
) ([]*rpc.TransactionSignature, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.signatures, nil
}

func (m *mockRPCClient) GetTransaction(
	ctx context.Context,
	signature solana.Signature,
	opts *rpc.GetTransactionOpts,
) (*rpc.GetTransactionResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.transactions == nil {
		return nil, nil
	}
	return m.transactions[signature.String()], nil
}

func newTestClient(mock *mockRPCClient) *Client {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewClient(mock, logger)
}

func TestGetTransactionsSince_NoLastSignature(t *testing.T) {
	ctx := context.Background()

	// Setup: Mock RPC returns 3 recent transactions
	sig1 := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	sig2 := solana.MustSignatureFromBase58("2TgM4N8qCMqLvfR8dxqTQgKygPNzT5KQkN5b5sT7eZPEkdxyLTXGnNQB3j7KG4DPFg5Qez5yNJBQRQ5r7DDnFfjG")
	sig3 := solana.MustSignatureFromBase58("3LzUfBWvh7uN5sNTVPkbDGq5SNrPBKDYTJqFmH8nHq6Z9VGJ7iCxB2rLFZsKrQNuJfTnKQ5D5YqGrNqvnKQZXMQE")

	now := solana.UnixTimeSeconds(time.Now().Unix())
	past1 := solana.UnixTimeSeconds(time.Now().Unix() - 10)
	past2 := solana.UnixTimeSeconds(time.Now().Unix() - 20)

	mock := &mockRPCClient{
		signatures: []*rpc.TransactionSignature{
			{
				Signature: sig1,
				Slot:      100,
				BlockTime: &now,
				Err:       nil,
			},
			{
				Signature: sig2,
				Slot:      99,
				BlockTime: &past1,
				Err:       nil,
			},
			{
				Signature: sig3,
				Slot:      98,
				BlockTime: &past2,
				Err:       nil,
			},
		},
	}

	client := newTestClient(mock)
	wallet := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")

	params := GetTransactionsSinceParams{
		Wallet:        wallet,
		LastSignature: nil, // Get latest
		Limit:         10,
	}

	// Act
	txns, err := client.GetTransactionsSince(ctx, params)

	// Assert
	require.NoError(t, err)
	require.Len(t, txns, 3)

	// Transactions should be in descending order (newest first)
	assert.Equal(t, sig1.String(), txns[0].Signature)
	assert.Equal(t, uint64(100), txns[0].Slot)
	assert.Equal(t, sig2.String(), txns[1].Signature)
	assert.Equal(t, uint64(99), txns[1].Slot)
	assert.Equal(t, sig3.String(), txns[2].Signature)
	assert.Equal(t, uint64(98), txns[2].Slot)
}

func TestGetTransactionsSince_WithLastSignature(t *testing.T) {
	ctx := context.Background()

	// Setup: Mock RPC returns transactions after the given signature
	sig1 := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	sig2 := solana.MustSignatureFromBase58("2TgM4N8qCMqLvfR8dxqTQgKygPNzT5KQkN5b5sT7eZPEkdxyLTXGnNQB3j7KG4DPFg5Qez5yNJBQRQ5r7DDnFfjG")
	lastSig := solana.MustSignatureFromBase58("3LzUfBWvh7uN5sNTVPkbDGq5SNrPBKDYTJqFmH8nHq6Z9VGJ7iCxB2rLFZsKrQNuJfTnKQ5D5YqGrNqvnKQZXMQE")

	now := solana.UnixTimeSeconds(time.Now().Unix())
	past1 := solana.UnixTimeSeconds(time.Now().Unix() - 10)

	mock := &mockRPCClient{
		signatures: []*rpc.TransactionSignature{
			{
				Signature: sig1,
				Slot:      100,
				BlockTime: &now,
			},
			{
				Signature: sig2,
				Slot:      99,
				BlockTime: &past1,
			},
		},
	}

	client := newTestClient(mock)
	wallet := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")

	params := GetTransactionsSinceParams{
		Wallet:        wallet,
		LastSignature: &lastSig,
		Limit:         10,
	}

	// Act
	txns, err := client.GetTransactionsSince(ctx, params)

	// Assert
	require.NoError(t, err)
	require.Len(t, txns, 2)
	assert.Equal(t, sig1.String(), txns[0].Signature)
	assert.Equal(t, sig2.String(), txns[1].Signature)
}

func TestGetTransactionsSince_EmptyResult(t *testing.T) {
	ctx := context.Background()

	mock := &mockRPCClient{
		signatures: []*rpc.TransactionSignature{},
	}

	client := newTestClient(mock)
	wallet := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")

	params := GetTransactionsSinceParams{
		Wallet: wallet,
		Limit:  10,
	}

	// Act
	txns, err := client.GetTransactionsSince(ctx, params)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, txns)
}

func TestGetTransactionsSince_ErrorFromRPC(t *testing.T) {
	ctx := context.Background()

	mock := &mockRPCClient{
		err: assert.AnError,
	}

	client := newTestClient(mock)
	wallet := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")

	params := GetTransactionsSinceParams{
		Wallet: wallet,
		Limit:  10,
	}

	// Act
	txns, err := client.GetTransactionsSince(ctx, params)

	// Assert
	require.Error(t, err)
	assert.Nil(t, txns)
}

func TestGetTransactionsSince_FailedTransaction(t *testing.T) {
	ctx := context.Background()

	// Setup: One transaction succeeded, one failed
	sig1 := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	sig2 := solana.MustSignatureFromBase58("2TgM4N8qCMqLvfR8dxqTQgKygPNzT5KQkN5b5sT7eZPEkdxyLTXGnNQB3j7KG4DPFg5Qez5yNJBQRQ5r7DDnFfjG")

	now := solana.UnixTimeSeconds(time.Now().Unix())
	past1 := solana.UnixTimeSeconds(time.Now().Unix() - 10)

	mock := &mockRPCClient{
		signatures: []*rpc.TransactionSignature{
			{
				Signature: sig1,
				Slot:      100,
				BlockTime: &now,
				Err:       nil,
			},
			{
				Signature: sig2,
				Slot:      99,
				BlockTime: &past1,
				Err:       map[string]interface{}{"InstructionError": []interface{}{0, "Custom error"}},
			},
		},
	}

	client := newTestClient(mock)
	wallet := solana.MustPublicKeyFromBase58("11111111111111111111111111111111")

	params := GetTransactionsSinceParams{
		Wallet: wallet,
		Limit:  10,
	}

	// Act
	txns, err := client.GetTransactionsSince(ctx, params)

	// Assert
	require.NoError(t, err)
	require.Len(t, txns, 2)

	// First transaction should have no error
	assert.Nil(t, txns[0].Err)

	// Second transaction should have an error
	assert.NotNil(t, txns[1].Err)
}
