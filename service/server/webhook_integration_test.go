package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebhookIntegration_EndToEnd exercises the full webhook->DB->NATS flow.
// Requires: TEST_DATABASE_URL env var and a running test postgres instance.
// Run with: RUN_DB_TESTS=1 go test -v -run TestWebhookIntegration_EndToEnd ./service/server/
func TestWebhookIntegration_EndToEnd(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_DB_TESTS=1 to run.")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Connect to test database
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	store := db.NewStore(pool)

	// Clean up test data
	_, err = pool.Exec(ctx, "DELETE FROM transactions WHERE signature LIKE 'test-webhook-%'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM wallets WHERE address LIKE 'TestWebhook%'")
	require.NoError(t, err)

	// Register a test wallet
	walletAddr := "TestWebhookWallet11111111111111111111111"
	_, err = store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:      walletAddr,
		Network:      "mainnet",
		AssetType:    "sol",
		TokenMint:    "",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)
	defer func() {
		pool.Exec(ctx, "DELETE FROM wallets WHERE address = $1", walletAddr)
	}()

	// Set up a mock NATS publisher to capture events
	pub := &mockPublisher{}

	// Create the webhook handler
	authToken := "Bearer test-integration-secret"
	handler := handleHeliusWebhook(store, pub, authToken, logger)

	// Simulate a Helius webhook delivery with a native SOL transfer TO our monitored wallet
	payload := []map[string]interface{}{
		{
			"signature": "test-webhook-sig-001",
			"slot":      uint64(999000),
			"timestamp": time.Now().Unix(),
			"fee":       5000,
			"feePayer":  "SenderWallet111111111111111111111111111111",
			"nativeTransfers": []map[string]interface{}{
				{
					"fromUserAccount": "SenderWallet111111111111111111111111111111",
					"toUserAccount":   walletAddr,
					"amount":          500_000_000, // 0.5 SOL
				},
			},
			"tokenTransfers":   []interface{}{},
			"accountData":      []interface{}{},
			"instructions":     []interface{}{},
			"transactionError": nil,
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(string(body)))
	req.Header.Set("Authorization", authToken)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "webhook handler should return 200")

	// Verify the transaction was written to the database
	txns, err := store.ListTransactionsByWallet(ctx, db.ListTransactionsByWalletParams{WalletAddress: walletAddr, Network: "mainnet", Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Len(t, txns, 1, "expected one transaction written to DB")

	txn := txns[0]
	assert.Equal(t, "test-webhook-sig-001", txn.Signature)
	assert.Equal(t, walletAddr, txn.WalletAddress)
	assert.Equal(t, "mainnet", txn.Network)
	assert.Equal(t, int64(500_000_000), txn.Amount)
	assert.Equal(t, "confirmed", txn.ConfirmationStatus)
	assert.Equal(t, "SenderWallet111111111111111111111111111111", *txn.FromAddress)

	// Verify NATS event was published
	require.Len(t, pub.events, 1, "expected one NATS event published")
	assert.Equal(t, "test-webhook-sig-001", pub.events[0].Signature)

	// Verify idempotency: re-send the same webhook
	req2 := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(string(body)))
	req2.Header.Set("Authorization", authToken)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Should still only have one transaction (duplicate was skipped)
	txns2, err := store.ListTransactionsByWallet(ctx, db.ListTransactionsByWalletParams{WalletAddress: walletAddr, Network: "mainnet", Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, txns2, 1, "duplicate should be skipped")

	// Clean up
	pool.Exec(ctx, "DELETE FROM transactions WHERE signature = 'test-webhook-sig-001'")

	t.Log("Integration test passed: webhook -> parse -> DB write -> NATS publish -> idempotency")
}

// TestWebhookIntegration_SPLToken tests the webhook flow for SPL token transfers.
func TestWebhookIntegration_SPLToken(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_DB_TESTS=1 to run.")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	store := db.NewStore(pool)

	// Clean up
	_, err = pool.Exec(ctx, "DELETE FROM transactions WHERE signature LIKE 'test-webhook-spl-%'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM wallets WHERE address LIKE 'TestSPLWallet%'")
	require.NoError(t, err)

	// Register a wallet monitoring USDC
	walletAddr := "TestSPLWallet1111111111111111111111111111"
	ataAddr := "TestSPLATA111111111111111111111111111111111"
	usdcMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	_, err = store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                walletAddr,
		Network:                "mainnet",
		AssetType:              "spl-token",
		TokenMint:              usdcMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           30 * time.Second,
		Status:                 "active",
	})
	require.NoError(t, err)
	defer func() {
		pool.Exec(ctx, "DELETE FROM wallets WHERE address = $1", walletAddr)
	}()

	pub := &mockPublisher{}
	authToken := "Bearer spl-test-secret"
	handler := handleHeliusWebhook(store, pub, authToken, logger)

	// Simulate a USDC transfer to our monitored ATA
	payload := []map[string]interface{}{
		{
			"signature": "test-webhook-spl-001",
			"slot":      uint64(999100),
			"timestamp": time.Now().Unix(),
			"fee":       5000,
			"feePayer":  "SenderWallet111111111111111111111111111111",
			"nativeTransfers": []interface{}{},
			"tokenTransfers": []map[string]interface{}{
				{
					"fromUserAccount":  "SenderWallet111111111111111111111111111111",
					"fromTokenAccount": "SenderATA1111111111111111111111111111111111",
					"toUserAccount":    walletAddr,
					"toTokenAccount":   ataAddr,
					"mint":             usdcMint,
					"tokenAmount":      10.5, // 10.5 USDC
					"tokenStandard":    "Fungible",
				},
			},
			"accountData":      []interface{}{},
			"instructions":     []interface{}{},
			"transactionError": nil,
		},
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(string(body)))
	req.Header.Set("Authorization", authToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify transaction
	txns, err := store.ListTransactionsByWallet(ctx, db.ListTransactionsByWalletParams{WalletAddress: walletAddr, Network: "mainnet", Limit: 10, Offset: 0})
	require.NoError(t, err)
	require.Len(t, txns, 1)

	txn := txns[0]
	assert.Equal(t, "test-webhook-spl-001", txn.Signature)
	assert.Equal(t, walletAddr, txn.WalletAddress)
	assert.Equal(t, int64(10_500_000), txn.Amount) // 10.5 USDC = 10_500_000 (6 decimals)
	assert.Equal(t, usdcMint, *txn.TokenMint)
	assert.Equal(t, "SenderWallet111111111111111111111111111111", *txn.FromAddress)

	// Verify NATS
	require.Len(t, pub.events, 1)
	assert.Equal(t, "test-webhook-spl-001", pub.events[0].Signature)

	// Clean up
	pool.Exec(ctx, "DELETE FROM transactions WHERE signature = 'test-webhook-spl-001'")

	t.Log("SPL token integration test passed")
}

// TestWebhookIntegration_BatchMultipleTransactions tests multiple transactions in one webhook delivery.
func TestWebhookIntegration_BatchMultipleTransactions(t *testing.T) {
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_DB_TESTS=1 to run.")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, pool.Ping(ctx))

	store := db.NewStore(pool)

	// Clean up
	_, err = pool.Exec(ctx, "DELETE FROM transactions WHERE signature LIKE 'test-webhook-batch-%'")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM wallets WHERE address LIKE 'TestBatchWallet%'")
	require.NoError(t, err)

	walletAddr := "TestBatchWallet111111111111111111111111111"
	_, err = store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:      walletAddr,
		Network:      "mainnet",
		AssetType:    "sol",
		TokenMint:    "",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)
	defer func() {
		pool.Exec(ctx, "DELETE FROM wallets WHERE address = $1", walletAddr)
	}()

	pub := &mockPublisher{}
	authToken := "Bearer batch-test-secret"
	handler := handleHeliusWebhook(store, pub, authToken, logger)

	// Send 3 transactions in one batch
	now := time.Now().Unix()
	payload := make([]map[string]interface{}, 3)
	for i := range 3 {
		payload[i] = map[string]interface{}{
			"signature": fmt.Sprintf("test-webhook-batch-%03d", i),
			"slot":      uint64(999200 + i),
			"timestamp": now - int64(i),
			"fee":       5000,
			"feePayer":  "SenderWallet111111111111111111111111111111",
			"nativeTransfers": []map[string]interface{}{
				{
					"fromUserAccount": "SenderWallet111111111111111111111111111111",
					"toUserAccount":   walletAddr,
					"amount":          uint64((i + 1) * 100_000_000),
				},
			},
			"tokenTransfers":   []interface{}{},
			"accountData":      []interface{}{},
			"instructions":     []interface{}{},
			"transactionError": nil,
		}
	}

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(string(body)))
	req.Header.Set("Authorization", authToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify all 3 transactions were written
	txns, err := store.ListTransactionsByWallet(ctx, db.ListTransactionsByWalletParams{WalletAddress: walletAddr, Network: "mainnet", Limit: 10, Offset: 0})
	require.NoError(t, err)
	assert.Len(t, txns, 3, "all 3 batch transactions should be written")

	// Verify NATS batch publish
	assert.Len(t, pub.events, 3, "all 3 events should be published to NATS")

	// Clean up
	pool.Exec(ctx, "DELETE FROM transactions WHERE signature LIKE 'test-webhook-batch-%'")

	t.Log("Batch integration test passed: 3 transactions in one webhook delivery")
}

// Ensure mockPublisher satisfies the Publisher interface.
var _ natspkg.Publisher = (*mockPublisher)(nil)
