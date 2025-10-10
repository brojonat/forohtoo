package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTransaction(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond) // Truncate for comparison

	// Test creating a SOL transaction (no token mint)
	t.Run("create SOL transaction", func(t *testing.T) {
		memo := `{"workflow_id": "test-workflow-123"}`
		params := CreateTransactionParams{
			Signature:          "sig123",
			WalletAddress:      "wallet123",
			Slot:               12345,
			BlockTime:          now,
			Amount:             1000000,
			TokenMint:          nil,
			Memo:               &memo,
			ConfirmationStatus: "finalized",
		}

		txn, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, txn)

		assert.Equal(t, params.Signature, txn.Signature)
		assert.Equal(t, params.WalletAddress, txn.WalletAddress)
		assert.Equal(t, params.Slot, txn.Slot)
		assert.Equal(t, params.Amount, txn.Amount)
		assert.Nil(t, txn.TokenMint)
		assert.NotNil(t, txn.Memo)
		assert.Equal(t, memo, *txn.Memo)
		assert.Equal(t, "finalized", txn.ConfirmationStatus)
		assert.WithinDuration(t, now, txn.BlockTime, time.Microsecond)
		assert.WithinDuration(t, time.Now(), txn.CreatedAt, 5*time.Second)
	})

	// Test creating a SPL token transaction
	t.Run("create SPL token transaction", func(t *testing.T) {
		tokenMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC
		params := CreateTransactionParams{
			Signature:          "sig456",
			WalletAddress:      "wallet123",
			Slot:               12346,
			BlockTime:          now.Add(time.Minute),
			Amount:             1000000, // 1 USDC (6 decimals)
			TokenMint:          &tokenMint,
			Memo:               nil,
			ConfirmationStatus: "confirmed",
		}

		txn, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
		require.NotNil(t, txn)

		assert.Equal(t, params.Signature, txn.Signature)
		assert.NotNil(t, txn.TokenMint)
		assert.Equal(t, tokenMint, *txn.TokenMint)
		assert.Nil(t, txn.Memo)
	})

	// Test duplicate signature + block_time (should fail due to composite PK)
	t.Run("duplicate signature and block_time", func(t *testing.T) {
		params := CreateTransactionParams{
			Signature:          "sig123", // Already exists
			WalletAddress:      "wallet456",
			Slot:               12347,
			BlockTime:          now, // Same block_time as first transaction
			Amount:             2000000,
			ConfirmationStatus: "finalized",
		}

		_, err := store.CreateTransaction(ctx, params)
		require.Error(t, err)
	})
}

func TestGetTransaction(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()

	// Create a transaction first
	now := time.Now().UTC().Truncate(time.Microsecond)
	params := CreateTransactionParams{
		Signature:          "sig789",
		WalletAddress:      "wallet123",
		Slot:               12345,
		BlockTime:          now,
		Amount:             1000000,
		ConfirmationStatus: "finalized",
	}

	created, err := store.CreateTransaction(ctx, params)
	require.NoError(t, err)

	// Test retrieving the transaction
	t.Run("get existing transaction", func(t *testing.T) {
		txn, err := store.GetTransaction(ctx, "sig789")
		require.NoError(t, err)
		require.NotNil(t, txn)

		assert.Equal(t, created.Signature, txn.Signature)
		assert.Equal(t, created.WalletAddress, txn.WalletAddress)
		assert.Equal(t, created.Amount, txn.Amount)
	})

	// Test retrieving non-existent transaction
	t.Run("get non-existent transaction", func(t *testing.T) {
		_, err := store.GetTransaction(ctx, "nonexistent")
		require.Error(t, err)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})
}

func TestListTransactionsByWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	// Create multiple transactions for the same wallet
	wallet := "wallet123"
	for i := 0; i < 5; i++ {
		params := CreateTransactionParams{
			Signature:          "sig" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          now.Add(time.Duration(i) * time.Minute),
			Amount:             int64(1000000 * (i + 1)),
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Create transactions for a different wallet
	for i := 0; i < 3; i++ {
		params := CreateTransactionParams{
			Signature:          "sig" + string(rune('X'+i)),
			WalletAddress:      "wallet456",
			Slot:               int64(22345 + i),
			BlockTime:          now.Add(time.Duration(i) * time.Minute),
			Amount:             int64(2000000 * (i + 1)),
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Test listing with pagination
	t.Run("list with pagination", func(t *testing.T) {
		params := ListTransactionsByWalletParams{
			WalletAddress: wallet,
			Limit:         3,
			Offset:        0,
		}

		txns, err := store.ListTransactionsByWallet(ctx, params)
		require.NoError(t, err)
		assert.Len(t, txns, 3)

		// Should be ordered by block_time DESC (newest first)
		assert.Equal(t, "sigE", txns[0].Signature)
		assert.Equal(t, "sigD", txns[1].Signature)
		assert.Equal(t, "sigC", txns[2].Signature)
	})

	t.Run("list with offset", func(t *testing.T) {
		params := ListTransactionsByWalletParams{
			WalletAddress: wallet,
			Limit:         2,
			Offset:        3,
		}

		txns, err := store.ListTransactionsByWallet(ctx, params)
		require.NoError(t, err)
		assert.Len(t, txns, 2)

		assert.Equal(t, "sigB", txns[0].Signature)
		assert.Equal(t, "sigA", txns[1].Signature)
	})

	t.Run("list only returns transactions for the specified wallet", func(t *testing.T) {
		params := ListTransactionsByWalletParams{
			WalletAddress: wallet,
			Limit:         10,
			Offset:        0,
		}

		txns, err := store.ListTransactionsByWallet(ctx, params)
		require.NoError(t, err)
		assert.Len(t, txns, 5) // Only wallet123 transactions

		for _, txn := range txns {
			assert.Equal(t, wallet, txn.WalletAddress)
		}
	})
}

func TestListTransactionsByWalletAndTimeRange(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create transactions at different times
	wallet := "wallet789"
	times := []time.Time{
		baseTime,
		baseTime.Add(1 * time.Hour),
		baseTime.Add(2 * time.Hour),
		baseTime.Add(3 * time.Hour),
		baseTime.Add(4 * time.Hour),
	}

	for i, blockTime := range times {
		params := CreateTransactionParams{
			Signature:          "time" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          blockTime,
			Amount:             int64(1000000 * (i + 1)),
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Test time range query
	t.Run("query within time range", func(t *testing.T) {
		params := ListTransactionsByWalletAndTimeRangeParams{
			WalletAddress: wallet,
			StartTime:     baseTime.Add(1 * time.Hour),
			EndTime:       baseTime.Add(3 * time.Hour),
		}

		txns, err := store.ListTransactionsByWalletAndTimeRange(ctx, params)
		require.NoError(t, err)
		assert.Len(t, txns, 3) // Should include times at 1h, 2h, 3h

		// Should be ordered by block_time DESC
		assert.Equal(t, "timeD", txns[0].Signature) // 3h
		assert.Equal(t, "timeC", txns[1].Signature) // 2h
		assert.Equal(t, "timeB", txns[2].Signature) // 1h
	})
}

func TestCountTransactionsByWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	now := time.Now().UTC()

	wallet := "walletCount"

	// Create 7 transactions
	for i := 0; i < 7; i++ {
		params := CreateTransactionParams{
			Signature:          "count" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          now.Add(time.Duration(i) * time.Minute),
			Amount:             1000000,
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	count, err := store.CountTransactionsByWallet(ctx, wallet)
	require.NoError(t, err)
	assert.Equal(t, int64(7), count)

	// Count for wallet with no transactions
	count, err = store.CountTransactionsByWallet(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestGetLatestTransactionByWallet(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	wallet := "walletLatest"

	// Create transactions with increasing block times
	for i := 0; i < 3; i++ {
		params := CreateTransactionParams{
			Signature:          "latest" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          now.Add(time.Duration(i) * time.Minute),
			Amount:             1000000,
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	txn, err := store.GetLatestTransactionByWallet(ctx, wallet)
	require.NoError(t, err)
	assert.Equal(t, "latestC", txn.Signature) // Should be the last one created
}

func TestGetTransactionsSince(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	wallet := "walletSince"

	// Create transactions at different times
	times := []time.Time{
		baseTime,
		baseTime.Add(10 * time.Minute),
		baseTime.Add(20 * time.Minute),
		baseTime.Add(30 * time.Minute),
	}

	for i, blockTime := range times {
		params := CreateTransactionParams{
			Signature:          "since" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          blockTime,
			Amount:             1000000,
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Get transactions since 15 minutes after base time
	since := baseTime.Add(15 * time.Minute)
	txns, err := store.GetTransactionsSince(ctx, wallet, since)
	require.NoError(t, err)

	// Should get transactions at 20min and 30min (but not 10min)
	assert.Len(t, txns, 2)

	// Should be ordered by block_time ASC (oldest first)
	assert.Equal(t, "sinceC", txns[0].Signature)
	assert.Equal(t, "sinceD", txns[1].Signature)
}

func TestDeleteTransactionsOlderThan(t *testing.T) {
	SkipIfNoTestDB(t)

	store := NewTestStore(t)
	defer store.Close()
	defer store.Cleanup(t)

	ctx := context.Background()
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	wallet := "walletDelete"

	// Create old transactions
	for i := 0; i < 3; i++ {
		params := CreateTransactionParams{
			Signature:          "old" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(12345 + i),
			BlockTime:          baseTime.Add(time.Duration(i) * time.Hour),
			Amount:             1000000,
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Create newer transactions
	for i := 0; i < 2; i++ {
		params := CreateTransactionParams{
			Signature:          "new" + string(rune('A'+i)),
			WalletAddress:      wallet,
			Slot:               int64(22345 + i),
			BlockTime:          baseTime.Add(time.Duration(10+i) * time.Hour),
			Amount:             1000000,
			ConfirmationStatus: "finalized",
		}
		_, err := store.CreateTransaction(ctx, params)
		require.NoError(t, err)
	}

	// Delete transactions older than 5 hours
	cutoff := baseTime.Add(5 * time.Hour)
	err := store.DeleteTransactionsOlderThan(ctx, cutoff)
	require.NoError(t, err)

	// Verify old transactions are deleted
	count, err := store.CountTransactionsByWallet(ctx, wallet)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count) // Only the 2 newer transactions remain

	// Verify we can still get the newer transactions
	params := ListTransactionsByWalletParams{
		WalletAddress: wallet,
		Limit:         10,
		Offset:        0,
	}
	txns, err := store.ListTransactionsByWallet(ctx, params)
	require.NoError(t, err)
	assert.Len(t, txns, 2)
	assert.Equal(t, "newB", txns[0].Signature)
	assert.Equal(t, "newA", txns[1].Signature)
}
