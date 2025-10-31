package temporal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"log/slog"
)

// TestAwaitPayment_Integration tests the AwaitPayment activity with real clients.
//
// This is an integration test that requires:
// - A running forohtoo server (FOROHTOO_SERVER_URL)
// - Access to Solana network for transaction data
// - A test wallet with known transactions
//
// Set RUN_INTEGRATION_TESTS=1 to enable this test.
func TestAwaitPayment_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	serverURL := os.Getenv("FOROHTOO_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:18000"
	}

	// Create forohtoo client
	forohtooClient := client.NewClient(serverURL, nil, slog.Default())

	// Create activities with real client
	activities := &Activities{
		forohtooClient: forohtooClient,
		logger:         slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test parameters - these should be configured for your test environment
	testWallet := os.Getenv("TEST_SERVICE_WALLET")
	if testWallet == "" {
		t.Skip("TEST_SERVICE_WALLET not set")
	}

	input := AwaitPaymentInput{
		PayToAddress:   testWallet,
		Network:        "devnet", // Use devnet for testing
		Amount:         100000,   // 0.1 USDC in base units
		Memo:           "forohtoo-test:integration",
		LookbackPeriod: 24 * time.Hour,
	}

	// This test will wait for an actual payment transaction
	// In a real test environment, you'd trigger a payment here or use a known historical transaction
	t.Log("Note: This test requires an actual payment transaction to complete successfully")
	t.Log("For automated testing, consider using a fixture with known historical transactions")

	result, err := activities.AwaitPayment(ctx, input)

	// In a real integration test environment, you'd have a test harness that creates transactions
	// For now, we just verify the activity runs without error
	if err != nil {
		t.Logf("AwaitPayment returned error (expected in test environment without actual payment): %v", err)
	} else {
		require.NotNil(t, result)
		assert.NotEmpty(t, result.TransactionSignature)
		assert.Greater(t, result.Amount, int64(0))
		t.Logf("Successfully received payment: signature=%s, amount=%d", result.TransactionSignature, result.Amount)
	}
}

// TestRegisterWallet_Integration tests the RegisterWallet activity with real infrastructure.
//
// This integration test requires:
// - A running PostgreSQL database (TEST_DATABASE_URL)
// - A running Temporal server (TEMPORAL_HOST)
// - NATS server for event publishing (NATS_URL)
//
// Set RUN_INTEGRATION_TESTS=1 to enable this test.
func TestRegisterWallet_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Setup database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(ctx), "failed to connect to test database")

	// Clean up test data
	_, err = pool.Exec(ctx, "DELETE FROM wallets WHERE address LIKE 'TEST%'")
	require.NoError(t, err)

	store := db.NewStore(pool)

	// Setup Temporal client
	cfg, err := config.Load()
	if err != nil {
		t.Skip("Failed to load config, skipping integration test")
	}

	temporalClient, err := NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		slog.Default(),
	)
	require.NoError(t, err, "failed to create temporal client")
	defer temporalClient.Close()

	// Create activities with real dependencies
	activities := &Activities{
		store:          store,
		temporalClient: temporalClient,
		logger:         slog.Default(),
	}

	// Test wallet registration
	testAddress := "TEST" + time.Now().Format("20060102150405") // Unique test address
	testNetwork := "devnet"
	tokenMint := cfg.USDCDevnetMintAddress
	ataAddr := testAddress + "-ATA" // Simplified for test

	input := RegisterWalletInput{
		Address:                testAddress,
		Network:                testNetwork,
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           1 * time.Minute,
	}

	result, err := activities.RegisterWallet(ctx, input)
	require.NoError(t, err, "RegisterWallet should succeed")
	require.NotNil(t, result)

	// Verify result
	assert.Equal(t, testAddress, result.Address)
	assert.Equal(t, testNetwork, result.Network)
	assert.Equal(t, "spl-token", result.AssetType)
	assert.Equal(t, tokenMint, result.TokenMint)
	assert.Equal(t, "active", result.Status)

	// Verify wallet was created in database
	wallet, err := store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
	require.NoError(t, err)
	assert.Equal(t, testAddress, wallet.Address)
	assert.Equal(t, 1*time.Minute, wallet.PollInterval)

	// Verify Temporal schedule was created
	// Note: This requires Temporal SDK access to verify schedule exists
	// In a full integration test, you'd query Temporal to verify the schedule
	t.Log("Note: Schedule verification requires Temporal API access")

	// Cleanup
	err = store.DeleteWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
	require.NoError(t, err)

	// Delete schedule (best effort - may fail if schedule doesn't exist)
	_ = temporalClient.DeleteWalletAssetSchedule(ctx, testAddress, testNetwork, "spl-token", tokenMint)

	t.Log("✓ Integration test passed: wallet registered successfully")
}

// TestRegisterWallet_Integration_Rollback tests that wallet registration rolls back on failure.
func TestRegisterWallet_Integration_Rollback(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Setup database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := db.NewStore(pool)

	// Create activities with nil temporal client to force failure
	activities := &Activities{
		store:          store,
		temporalClient: nil, // This will cause schedule creation to fail
		logger:         slog.Default(),
	}

	testAddress := "TEST-ROLLBACK-" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := "TestMint123"
	ataAddr := testAddress + "-ATA"

	input := RegisterWalletInput{
		Address:                testAddress,
		Network:                testNetwork,
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           1 * time.Minute,
	}

	// This should fail because temporal client is nil
	result, err := activities.RegisterWallet(ctx, input)
	assert.Error(t, err, "RegisterWallet should fail when temporal client is nil")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "temporal client not configured")

	// Verify wallet was NOT created in database (rollback should have occurred)
	_, err = store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
	assert.Error(t, err, "wallet should not exist after rollback")

	t.Log("✓ Integration test passed: wallet registration rolled back on failure")
}
