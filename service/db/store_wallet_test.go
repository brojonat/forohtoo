package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	params := CreateWalletParams{
		Address:      "wallet123",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	}

	wallet, err := store.CreateWallet(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, wallet)

	assert.Equal(t, params.Address, wallet.Address)
	assert.Equal(t, params.Network, wallet.Network)
	assert.Equal(t, params.PollInterval, wallet.PollInterval)
	assert.Equal(t, params.Status, wallet.Status)
	assert.Nil(t, wallet.LastPollTime)
	assert.False(t, wallet.CreatedAt.IsZero())
	assert.False(t, wallet.UpdatedAt.IsZero())
}

func TestCreateWallet_DuplicateAddress(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	params := CreateWalletParams{
		Address:      "wallet123",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	}

	// Create first wallet
	_, err := store.CreateWallet(ctx, params)
	require.NoError(t, err)

	// Try to create duplicate (same address + network)
	_, err = store.CreateWallet(ctx, params)
	require.Error(t, err)
	// Should be a unique constraint violation

	// But can create same address on different network
	paramsDevnet := CreateWalletParams{
		Address:      "wallet123",
		Network:      "devnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	}
	_, err = store.CreateWallet(ctx, paramsDevnet)
	require.NoError(t, err, "should allow same address on different network")
}

func TestGetWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create wallet first
	created, err := store.CreateWallet(ctx, CreateWalletParams{
		Address:      "wallet456",
		Network:      "mainnet",
		PollInterval: 60 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Get wallet
	wallet, err := store.GetWallet(ctx, "wallet456", "mainnet")
	require.NoError(t, err)
	require.NotNil(t, wallet)

	assert.Equal(t, created.Address, wallet.Address)
	assert.Equal(t, created.Network, wallet.Network)
	assert.Equal(t, created.PollInterval, wallet.PollInterval)
	assert.Equal(t, created.Status, wallet.Status)
}

func TestGetWallet_NotFound(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	wallet, err := store.GetWallet(ctx, "nonexistent", "mainnet")
	require.Error(t, err)
	assert.Nil(t, wallet)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestListWallets(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create multiple wallets on different networks
	wallets := []CreateWalletParams{
		{Address: "wallet1", Network: "mainnet", PollInterval: 30 * time.Second, Status: "active"},
		{Address: "wallet2", Network: "mainnet", PollInterval: 30 * time.Second, Status: "active"},
		{Address: "wallet1", Network: "devnet", PollInterval: 30 * time.Second, Status: "active"},
	}
	for _, params := range wallets {
		_, err := store.CreateWallet(ctx, params)
		require.NoError(t, err)
	}

	// List all wallets
	allWallets, err := store.ListWallets(ctx)
	require.NoError(t, err)
	require.Len(t, allWallets, 3, "should list wallets from all networks")

	// Should be ordered by created_at DESC
	assert.Equal(t, "wallet1", allWallets[0].Address)
	assert.Equal(t, "devnet", allWallets[0].Network)
}

func TestListWallets_Empty(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	wallets, err := store.ListWallets(ctx)
	require.NoError(t, err)
	assert.Empty(t, wallets)
}

func TestListActiveWallets(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create active wallets
	_, err := store.CreateWallet(ctx, CreateWalletParams{
		Address:      "active1",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	_, err = store.CreateWallet(ctx, CreateWalletParams{
		Address:      "active2",
		Network:      "devnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Create paused wallet
	_, err = store.CreateWallet(ctx, CreateWalletParams{
		Address:      "paused1",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "paused",
	})
	require.NoError(t, err)

	// List only active wallets
	wallets, err := store.ListActiveWallets(ctx)
	require.NoError(t, err)
	require.Len(t, wallets, 2)

	// Should only contain active wallets
	for _, w := range wallets {
		assert.Equal(t, "active", w.Status)
	}
}

func TestUpdateWalletPollTime(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create wallet
	wallet, err := store.CreateWallet(ctx, CreateWalletParams{
		Address:      "wallet789",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)
	assert.Nil(t, wallet.LastPollTime)

	// Update poll time
	now := time.Now()
	updated, err := store.UpdateWalletPollTime(ctx, "wallet789", "mainnet", now)
	require.NoError(t, err)
	require.NotNil(t, updated.LastPollTime)

	assert.Equal(t, "wallet789", updated.Address)
	assert.Equal(t, "mainnet", updated.Network)
	assert.WithinDuration(t, now, *updated.LastPollTime, time.Second)
	assert.True(t, updated.UpdatedAt.After(wallet.UpdatedAt))
}

func TestUpdateWalletStatus(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create wallet
	wallet, err := store.CreateWallet(ctx, CreateWalletParams{
		Address:      "wallet999",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)
	assert.Equal(t, "active", wallet.Status)

	// Update status to paused
	updated, err := store.UpdateWalletStatus(ctx, "wallet999", "mainnet", "paused")
	require.NoError(t, err)
	assert.Equal(t, "paused", updated.Status)
	assert.True(t, updated.UpdatedAt.After(wallet.UpdatedAt))

	// Verify status was persisted
	fetched, err := store.GetWallet(ctx, "wallet999", "mainnet")
	require.NoError(t, err)
	assert.Equal(t, "paused", fetched.Status)
}

func TestDeleteWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create wallet
	_, err := store.CreateWallet(ctx, CreateWalletParams{
		Address:      "wallet111",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Delete wallet
	err = store.DeleteWallet(ctx, "wallet111", "mainnet")
	require.NoError(t, err)

	// Verify deletion
	wallet, err := store.GetWallet(ctx, "wallet111", "mainnet")
	require.Error(t, err)
	assert.Nil(t, wallet)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestDeleteWallet_NotFound(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Delete non-existent wallet should not error (idempotent)
	err := store.DeleteWallet(ctx, "nonexistent", "mainnet")
	require.NoError(t, err)
}

func TestWalletExists(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Check non-existent wallet
	exists, err := store.WalletExists(ctx, "wallet222", "mainnet")
	require.NoError(t, err)
	assert.False(t, exists)

	// Create wallet
	_, err = store.CreateWallet(ctx, CreateWalletParams{
		Address:      "wallet222",
		Network:      "mainnet",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Check existing wallet
	exists, err = store.WalletExists(ctx, "wallet222", "mainnet")
	require.NoError(t, err)
	assert.True(t, exists)

	// Same address on different network should not exist
	exists, err = store.WalletExists(ctx, "wallet222", "devnet")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestWalletPollIntervalConversion(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	testCases := []struct {
		name     string
		interval time.Duration
	}{
		{"30 seconds", 30 * time.Second},
		{"1 minute", time.Minute},
		{"5 minutes", 5 * time.Minute},
		{"1 hour", time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			address := "wallet_" + tc.name

			wallet, err := store.CreateWallet(ctx, CreateWalletParams{
				Address:      address,
				Network:      "mainnet",
				PollInterval: tc.interval,
				Status:       "active",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.interval, wallet.PollInterval)

			// Verify roundtrip
			fetched, err := store.GetWallet(ctx, address, "mainnet")
			require.NoError(t, err)
			assert.Equal(t, tc.interval, fetched.PollInterval)
		})
	}
}
