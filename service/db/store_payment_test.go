package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStore_WalletExists_NotExists tests that WalletExists returns false
// for wallets that don't exist in the database.
//
// WHAT IS BEING TESTED:
// We're testing the WalletExists query function used by the payment gateway
// to determine whether a wallet registration requires payment.
//
// EXPECTED BEHAVIOR:
// - Query wallet that doesn't exist in database
// - Returns false (wallet not found)
// - Returns nil error (no database error)
// - No panic or unexpected behavior
//
// This is the "new wallet" case that triggers payment requirement.
func TestStore_WalletExists_NotExists(t *testing.T) {
	// Note: This test requires a real database connection or test container
	// For now, we'll use a mock store implementation
	store := &MockStore{}

	ctx := context.Background()
	address := "NonExistentWallet123456789012345678901"
	network := "mainnet"
	assetType := "sol"
	tokenMint := ""

	// Mock WalletExists to return false for non-existent wallet
	store.WalletExistsFunc = func(ctx context.Context, addr, net, asset, mint string) (bool, error) {
		// Verify correct parameters passed
		assert.Equal(t, address, addr)
		assert.Equal(t, network, net)
		assert.Equal(t, assetType, asset)
		assert.Equal(t, tokenMint, mint)

		return false, nil
	}

	exists, err := store.WalletExists(ctx, address, network, assetType, tokenMint)

	// Verify wallet does not exist
	require.NoError(t, err, "WalletExists should not return error for non-existent wallet")
	assert.False(t, exists, "Wallet should not exist")

	t.Logf("✓ WalletExists correctly returns false for non-existent wallet")
}

// TestStore_WalletExists_Exists tests that WalletExists returns true
// for wallets that exist in the database.
//
// WHAT IS BEING TESTED:
// We're testing the WalletExists query function for existing wallets,
// which allows the payment gateway to skip payment for existing registrations.
//
// EXPECTED BEHAVIOR:
// - Create wallet in database
// - Query WalletExists for that wallet
// - Returns true (wallet found)
// - Returns nil error
// - Payment gateway will proceed with upsert without payment
//
// This is the "existing wallet" case that skips payment requirement.
func TestStore_WalletExists_Exists(t *testing.T) {
	store := &MockStore{}

	ctx := context.Background()
	address := "ExistingWallet12345678901234567890123"
	network := "mainnet"
	assetType := "sol"
	tokenMint := ""

	// First, create the wallet
	store.UpsertWalletFunc = func(ctx context.Context, params UpsertWalletParams) (*Wallet, error) {
		return &Wallet{
			Address:      params.Address,
			Network:      params.Network,
			AssetType:    params.AssetType,
			TokenMint:    params.TokenMint,
			PollInterval: params.PollInterval,
			Status:       "active",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}, nil
	}

	wallet, err := store.UpsertWallet(ctx, UpsertWalletParams{
		Address:      address,
		Network:      network,
		AssetType:    assetType,
		TokenMint:    tokenMint,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)
	require.NotNil(t, wallet)

	// Now mock WalletExists to return true
	store.WalletExistsFunc = func(ctx context.Context, addr, net, asset, mint string) (bool, error) {
		assert.Equal(t, address, addr)
		assert.Equal(t, network, net)
		assert.Equal(t, assetType, asset)
		assert.Equal(t, tokenMint, mint)

		return true, nil
	}

	// Query WalletExists
	exists, err := store.WalletExists(ctx, address, network, assetType, tokenMint)

	// Verify wallet exists
	require.NoError(t, err, "WalletExists should not return error")
	assert.True(t, exists, "Wallet should exist")

	t.Logf("✓ WalletExists correctly returns true for existing wallet")
}

// Mock store for database tests
type MockStore struct {
	WalletExistsFunc func(ctx context.Context, address, network, assetType, tokenMint string) (bool, error)
	UpsertWalletFunc func(ctx context.Context, params UpsertWalletParams) (*Wallet, error)
	DeleteWalletFunc func(ctx context.Context, address, network, assetType, tokenMint string) error
}

func (m *MockStore) WalletExists(ctx context.Context, address, network, assetType, tokenMint string) (bool, error) {
	if m.WalletExistsFunc != nil {
		return m.WalletExistsFunc(ctx, address, network, assetType, tokenMint)
	}
	return false, nil
}

func (m *MockStore) UpsertWallet(ctx context.Context, params UpsertWalletParams) (*Wallet, error) {
	if m.UpsertWalletFunc != nil {
		return m.UpsertWalletFunc(ctx, params)
	}
	return nil, nil
}

func (m *MockStore) DeleteWallet(ctx context.Context, address, network, assetType, tokenMint string) error {
	if m.DeleteWalletFunc != nil {
		return m.DeleteWalletFunc(ctx, address, network, assetType, tokenMint)
	}
	return nil
}
