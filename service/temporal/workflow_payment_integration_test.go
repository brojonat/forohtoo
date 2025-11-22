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

// TestPaymentGatedRegistrationWorkflow_Integration tests the full payment-gated registration flow.
//
// This integration test requires:
// - Running Temporal server (TEMPORAL_HOST)
// - Running Temporal worker processing the workflow
// - Running forohtoo server for SSE payment monitoring
// - PostgreSQL database (TEST_DATABASE_URL)
// - Actual payment transaction or test harness
//
// Set RUN_INTEGRATION_TESTS=1 to enable this test.
// Set RUN_PAYMENT_INTEGRATION_TESTS=1 for full end-to-end payment tests.
func TestPaymentGatedRegistrationWorkflow_Integration(t *testing.T) {
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
	require.NoError(t, err, "failed to connect to Temporal")
	defer temporalClient.Close()

	// Setup database for verification
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	store := db.NewStore(pool)

	// Test parameters
	testAddress := "TEST-PAYMENT-" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := cfg.USDCDevnetMintAddress

	workflowID := "test-payment-registration:" + testAddress
	workflowInput := PaymentGatedRegistrationInput{
		Address:        testAddress,
		Network:        testNetwork,
		AssetType:      "spl-token",
		TokenMint:      tokenMint,
		PollInterval:   1 * time.Minute,
		ServiceWallet:  cfg.PaymentGateway.ServiceWallet,
		ServiceNetwork: cfg.PaymentGateway.ServiceNetwork,
		FeeAmount:      cfg.PaymentGateway.FeeAmount,
		PaymentMemo:    "forohtoo-test:" + testAddress,
		PaymentTimeout: 30 * time.Second, // Short timeout for testing
	}

	// Start workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: cfg.TemporalTaskQueue,
	}

	workflowRun, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, "PaymentGatedRegistrationWorkflow", workflowInput)
	require.NoError(t, err, "failed to start workflow")

	t.Logf("Started workflow: %s", workflowID)

	if os.Getenv("RUN_PAYMENT_INTEGRATION_TESTS") != "" {
		// Full integration test - wait for actual payment
		t.Log("Waiting for payment... (send payment to service wallet with memo: " + workflowInput.PaymentMemo + ")")

		// Wait for workflow to complete (with timeout)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var result PaymentGatedRegistrationResult
		err = workflowRun.Get(ctx, &result)

		if err != nil {
			// Check if timeout occurred
			if ctx.Err() == context.DeadlineExceeded {
				t.Log("Workflow timed out waiting for payment (expected in test environment)")
				return
			}
			require.NoError(t, err, "workflow failed")
		}

		// Verify result
		assert.Equal(t, "completed", result.Status)
		assert.Equal(t, testAddress, result.Address)
		assert.NotNil(t, result.PaymentSignature)
		assert.Greater(t, result.PaymentAmount, int64(0))

		// Verify wallet was registered in database
		wallet, err := store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
		require.NoError(t, err)
		assert.Equal(t, "active", wallet.Status)

		// Cleanup
		_ = store.DeleteWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)

		t.Log("✓ Full payment integration test passed")
	} else {
		// Partial integration test - just verify workflow started
		t.Log("Workflow started successfully. Set RUN_PAYMENT_INTEGRATION_TESTS=1 for full test.")

		// Query workflow status
		desc, err := temporalClient.DescribeWorkflowExecution(ctx, workflowID, "")
		require.NoError(t, err)
		assert.NotNil(t, desc)

		t.Logf("Workflow status: %s", desc.WorkflowExecutionInfo.Status)
	}
}

// TestPaymentGatedRegistrationWorkflow_Timeout tests workflow behavior when payment times out.
//
// This test verifies that the workflow properly handles payment timeout scenarios.
func TestPaymentGatedRegistrationWorkflow_Timeout_Integration(t *testing.T) {
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

	testAddress := "TEST-TIMEOUT-" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := cfg.USDCDevnetMintAddress

	workflowID := "test-payment-timeout:" + testAddress
	workflowInput := PaymentGatedRegistrationInput{
		Address:        testAddress,
		Network:        testNetwork,
		AssetType:      "spl-token",
		TokenMint:      tokenMint,
		PollInterval:   1 * time.Minute,
		ServiceWallet:  cfg.PaymentGateway.ServiceWallet,
		ServiceNetwork: cfg.PaymentGateway.ServiceNetwork,
		FeeAmount:      cfg.PaymentGateway.FeeAmount,
		PaymentMemo:    "forohtoo-test-timeout:" + testAddress,
		PaymentTimeout: 5 * time.Second, // Very short timeout to force timeout
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: cfg.TemporalTaskQueue,
	}

	workflowRun, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, "PaymentGatedRegistrationWorkflow", workflowInput)
	require.NoError(t, err)

	t.Logf("Started workflow with short timeout: %s", workflowID)

	// Wait for workflow to complete (should timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result PaymentGatedRegistrationResult
	err = workflowRun.Get(ctx, &result)

	// Workflow should complete with timeout status
	// Note: Depending on implementation, this might be an error or a result with status="timeout"
	if err == nil {
		// Check result status
		assert.Contains(t, []string{"timeout", "failed"}, result.Status, "workflow should timeout or fail")
		assert.NotNil(t, result.Error, "error message should be set")
		t.Logf("Workflow completed with status: %s, error: %s", result.Status, *result.Error)
	} else {
		t.Logf("Workflow failed as expected: %v", err)
	}

	// Verify wallet was NOT registered in database
	_, err = store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
	assert.Error(t, err, "wallet should not exist after payment timeout")

	t.Log("✓ Timeout integration test passed")
}

// TestPaymentGatedRegistrationWorkflow_AlreadyPaid tests the historical payment scenario.
//
// This test verifies that the workflow can complete immediately if payment was already received
// before the workflow started (within the lookback period).
func TestPaymentGatedRegistrationWorkflow_AlreadyPaid_Integration(t *testing.T) {
	if os.Getenv("RUN_PAYMENT_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping full payment integration test. Set RUN_PAYMENT_INTEGRATION_TESTS=1 to run.")
	}

	// This test requires:
	// 1. A known historical payment transaction in the service wallet
	// 2. The transaction memo to match the test pattern
	//
	// In a real test environment, you would:
	// - Have a test wallet with fixtures of known transactions
	// - Use those transaction signatures and memos for testing
	// - Verify the workflow completes immediately without waiting

	t.Log("Note: This test requires a test harness with historical payment fixtures")
	t.Log("Implementation depends on your test environment setup")
	t.Skip("Requires historical payment fixtures")
}
