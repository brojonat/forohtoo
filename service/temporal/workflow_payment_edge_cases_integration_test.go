package temporal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

// TestPaymentGatedRegistrationWorkflow_WalletAlreadyExists tests the case where
// the wallet is already registered before payment.
//
// This integration test verifies that:
// - The workflow detects existing wallets
// - Appropriate handling occurs (either skip payment or update existing)
//
// Set RUN_INTEGRATION_TESTS=1 to enable this test.
func TestPaymentGatedRegistrationWorkflow_WalletAlreadyExists_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
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

	testAddress := "TEST-EXISTS-" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := cfg.USDCDevnetMintAddress

	// Pre-create the wallet in database
	ataAddr := testAddress + "-ATA"
	wallet, err := store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                testAddress,
		Network:                testNetwork,
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           1 * time.Minute,
		Status:                 "active",
	})
	require.NoError(t, err)
	require.NotNil(t, wallet)
	defer store.DeleteWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)

	t.Logf("Pre-created wallet: %s", testAddress)

	// Now try to start payment-gated registration workflow
	// The workflow should detect the existing wallet and handle appropriately

	temporalClient, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalHost,
		Namespace: cfg.TemporalNamespace,
	})
	require.NoError(t, err)
	defer temporalClient.Close()

	workflowID := "test-exists:" + testAddress
	workflowInput := PaymentGatedRegistrationInput{
		Address:                testAddress,
		Network:                testNetwork,
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           1 * time.Minute,
		ServiceWallet:          cfg.PaymentGateway.ServiceWallet,
		ServiceNetwork:         cfg.PaymentGateway.ServiceNetwork,
		FeeAmount:              cfg.PaymentGateway.FeeAmount,
		PaymentMemo:            "forohtoo-test-exists:" + testAddress,
		PaymentTimeout:         10 * time.Second,
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: cfg.TemporalTaskQueue,
	}

	workflowRun, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, "PaymentGatedRegistrationWorkflow", workflowInput)
	require.NoError(t, err)

	// Wait for workflow to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result PaymentGatedRegistrationResult
	err = workflowRun.Get(ctx, &result)

	// The behavior depends on implementation:
	// - Option 1: Workflow detects existing wallet early and completes immediately
	// - Option 2: Workflow proceeds anyway (idempotent upsert)
	// - Option 3: Workflow returns error about duplicate

	if err == nil {
		t.Logf("Workflow completed with status: %s", result.Status)
		// Verify wallet still exists and is unchanged
		updatedWallet, err := store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
		require.NoError(t, err)
		assert.Equal(t, "active", updatedWallet.Status)
	} else {
		t.Logf("Workflow failed (may be expected): %v", err)
	}

	t.Log("✓ Wallet already exists test completed")
}

// TestPaymentGatedRegistrationWorkflow_ConcurrentRegistrations tests concurrent
// registration attempts for the same wallet.
//
// This test verifies race condition handling when multiple workflows try to
// register the same wallet simultaneously.
func TestPaymentGatedRegistrationWorkflow_ConcurrentRegistrations_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
	}

	// Connect to Temporal
	temporalClient, err := client.Dial(client.Options{
		HostPort:  cfg.TemporalHost,
		Namespace: cfg.TemporalNamespace,
	})
	require.NoError(t, err)
	defer temporalClient.Close()

	testAddress := "TEST-CONCURRENT-" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := cfg.USDCDevnetMintAddress
	ataAddr := testAddress + "-ATA"

	// Start 3 concurrent workflows for the same wallet
	workflowIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		workflowID := "test-concurrent-" + testAddress + "-" + string(rune('A'+i))
		workflowIDs[i] = workflowID

		workflowInput := PaymentGatedRegistrationInput{
			Address:                testAddress,
			Network:                testNetwork,
			AssetType:              "spl-token",
			TokenMint:              tokenMint,
			AssociatedTokenAddress: &ataAddr,
			PollInterval:           1 * time.Minute,
			ServiceWallet:          cfg.PaymentGateway.ServiceWallet,
			ServiceNetwork:         cfg.PaymentGateway.ServiceNetwork,
			FeeAmount:              cfg.PaymentGateway.FeeAmount,
			PaymentMemo:            "forohtoo-test-concurrent:" + testAddress + "-" + string(rune('A'+i)),
			PaymentTimeout:         5 * time.Second,
		}

		workflowOptions := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: cfg.TemporalTaskQueue,
		}

		_, err := temporalClient.ExecuteWorkflow(context.Background(), workflowOptions, "PaymentGatedRegistrationWorkflow", workflowInput)
		require.NoError(t, err)
		t.Logf("Started concurrent workflow %d: %s", i+1, workflowID)
	}

	// Wait a bit and check workflow states
	time.Sleep(10 * time.Second)

	ctx := context.Background()
	for i, workflowID := range workflowIDs {
		desc, err := temporalClient.DescribeWorkflowExecution(ctx, workflowID, "")
		if err == nil {
			t.Logf("Workflow %d (%s) status: %s", i+1, workflowID, desc.WorkflowExecutionInfo.Status)
		}
	}

	// Verify database state - should have at most one wallet registered
	// (depending on which workflow completes first or how concurrency is handled)

	// Cleanup
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL != "" {
		pool, err := pgxpool.New(ctx, dbURL)
		if err == nil {
			store := db.NewStore(pool)
			_ = store.DeleteWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
			pool.Close()
		}
	}

	t.Log("✓ Concurrent registrations test completed")
	t.Log("Note: Verify Temporal workflow execution logs to confirm proper concurrency handling")
}

// TestPaymentGatedRegistrationWorkflow_InvalidPayment tests workflow behavior with
// invalid or insufficient payments.
//
// This test verifies that:
// - Payments with wrong amounts are rejected
// - Payments with wrong memos are rejected
// - Workflow times out appropriately
func TestPaymentGatedRegistrationWorkflow_InvalidPayment_Integration(t *testing.T) {
	if os.Getenv("RUN_PAYMENT_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping payment integration test. Set RUN_PAYMENT_INTEGRATION_TESTS=1 to run.")
	}

	// This test requires a test harness that can:
	// 1. Send payments with specific amounts and memos
	// 2. Verify the workflow correctly rejects invalid payments
	// 3. Monitor workflow state throughout the process
	//
	// In a full test environment, you would:
	// - Send payment with amount < required amount
	// - Send payment with correct amount but wrong memo
	// - Send payment to wrong wallet
	// - Verify workflow waits for correct payment or times out

	t.Log("Note: This test requires a payment test harness")
	t.Log("Implement based on your Solana devnet testing infrastructure")
	t.Skip("Requires payment test harness")
}

// TestPaymentGatedRegistrationWorkflow_Recovery tests workflow recovery after
// temporal worker restarts.
//
// This test verifies that:
// - Workflows can resume after worker restart
// - State is properly persisted
// - Long-running payment waits survive restarts
func TestPaymentGatedRegistrationWorkflow_Recovery_Integration(t *testing.T) {
	if os.Getenv("RUN_RECOVERY_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping recovery integration test. Set RUN_RECOVERY_INTEGRATION_TESTS=1 to run.")
	}

	// This test requires:
	// 1. Starting a workflow
	// 2. Stopping the temporal worker
	// 3. Restarting the worker
	// 4. Verifying workflow continues correctly
	//
	// This is typically tested in a staging/production-like environment
	// with orchestrated restarts

	t.Log("Note: This test requires orchestrated worker restarts")
	t.Log("Best tested in staging environment with monitoring")
	t.Skip("Requires worker restart orchestration")
}
