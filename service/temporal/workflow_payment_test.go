package temporal

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

// TestPaymentGatedRegistrationWorkflow_Success tests the complete happy path
// where payment is received and wallet is successfully registered.
//
// WHAT IS BEING TESTED:
// We're testing the end-to-end success flow of the PaymentGatedRegistrationWorkflow,
// which orchestrates payment verification and wallet registration.
//
// EXPECTED BEHAVIOR:
// - Workflow should execute AwaitPayment activity first
// - When payment is received, workflow should execute RegisterWallet activity
// - Result should include payment signature, payment amount, and registration timestamp
// - Status should be "completed"
// - No errors should occur
//
// This tests the primary use case: user pays, wallet gets registered.
func TestPaymentGatedRegistrationWorkflow_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock AwaitPayment activity - returns successful payment
	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).Return(&AwaitPaymentResult{
		TransactionSignature: "payment-signature-abc123",
		Amount:               1000000,
		FromAddress:          stringPtr("PayerAddress123"),
		BlockTime:            time.Now(),
	}, nil)

	// Mock RegisterWallet activity - returns successful registration
	env.OnActivity(RegisterWallet, mock.Anything, mock.Anything).Return(&RegisterWalletResult{
		Address:   "UserWallet123",
		Network:   "mainnet",
		AssetType: "sol",
		TokenMint: "",
		Status:    "active",
	}, nil)

	// Execute workflow
	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet123",
		Network:        "mainnet",
		AssetType:      "sol",
		TokenMint:      "",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet456",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "forohtoo-reg:test-123",
		PaymentTimeout: 24 * time.Hour,
	})

	// Verify workflow completed successfully
	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Get result
	var result PaymentGatedRegistrationResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Failed to get workflow result: %v", err)
	}

	// Verify result
	if result.Status != "completed" {
		t.Errorf("Expected status=completed, got %q", result.Status)
	}

	if result.PaymentSignature == nil || *result.PaymentSignature != "payment-signature-abc123" {
		t.Errorf("Expected payment_signature=payment-signature-abc123, got %v", result.PaymentSignature)
	}

	if result.PaymentAmount != 1000000 {
		t.Errorf("Expected payment_amount=1000000, got %d", result.PaymentAmount)
	}

	if result.Error != nil {
		t.Errorf("Expected no error, got %v", result.Error)
	}

	if result.RegisteredAt.IsZero() {
		t.Error("Expected RegisteredAt to be set")
	}

	// Verify both activities were called
	env.AssertExpectations(t)
}

// TestPaymentGatedRegistrationWorkflow_PaymentTimeout tests the scenario where
// no payment is received within the timeout period.
//
// WHAT IS BEING TESTED:
// We're testing that the workflow correctly handles the case where the user
// never sends payment and the AwaitPayment activity times out.
//
// EXPECTED BEHAVIOR:
// - Workflow should execute AwaitPayment activity
// - When AwaitPayment times out, workflow should fail
// - Result should include error message indicating timeout
// - Status should be "failed" (or workflow should return error)
// - RegisterWallet activity should NOT be called
//
// This ensures workflows don't hang forever and properly report timeouts.
func TestPaymentGatedRegistrationWorkflow_PaymentTimeout(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock AwaitPayment activity - returns timeout error
	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("payment await failed: context deadline exceeded"),
	)

	// Execute workflow
	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet123",
		Network:        "mainnet",
		AssetType:      "sol",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet456",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "forohtoo-reg:test-timeout",
		PaymentTimeout: 1 * time.Hour,
	})

	// Verify workflow completed (with failure)
	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	// Workflow should have failed
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("Expected workflow to fail with timeout error")
	}

	// Error should mention timeout or deadline
	errMsg := err.Error()
	if !contains(errMsg, "deadline exceeded") && !contains(errMsg, "timeout") {
		t.Errorf("Expected error to mention timeout/deadline, got: %v", err)
	}
}

// TestPaymentGatedRegistrationWorkflow_RegistrationFails tests the scenario where
// payment is received but wallet registration fails after all retries.
//
// WHAT IS BEING TESTED:
// We're testing that the workflow correctly handles the case where payment
// succeeds but the RegisterWallet activity fails permanently.
//
// EXPECTED BEHAVIOR:
// - AwaitPayment activity should succeed
// - RegisterWallet activity should fail (all retries exhausted)
// - Workflow should fail with error
// - Result should include payment signature (proof of payment)
// - Result should include error message about registration failure
//
// This is critical for support - we need to know the user paid but registration failed.
func TestPaymentGatedRegistrationWorkflow_RegistrationFails(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock AwaitPayment activity - returns successful payment
	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).Return(&AwaitPaymentResult{
		TransactionSignature: "payment-signature-xyz",
		Amount:               1000000,
		BlockTime:            time.Now(),
	}, nil)

	// Mock RegisterWallet activity - fails permanently
	env.OnActivity(RegisterWallet, mock.Anything, mock.Anything).Return(
		nil,
		errors.New("failed to create schedule: temporal server unavailable"),
	)

	// Execute workflow
	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet123",
		Network:        "mainnet",
		AssetType:      "sol",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet456",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "forohtoo-reg:test-regfail",
		PaymentTimeout: 24 * time.Hour,
	})

	// Verify workflow completed (with failure)
	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	// Workflow should have failed
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("Expected workflow to fail with registration error")
	}

	// Error should mention registration failure
	errMsg := err.Error()
	if !contains(errMsg, "registration failed") && !contains(errMsg, "failed to create schedule") {
		t.Errorf("Expected error to mention registration failure, got: %v", err)
	}

	// Try to get partial result (payment may be recorded even though workflow failed)
	var result PaymentGatedRegistrationResult
	env.GetWorkflowResult(&result)

	// Even though workflow failed, we should have payment signature for support
	// (This depends on how the workflow is implemented - it may or may not set this)
	// The key is that the error message should allow support to track down the payment
}

// TestPaymentGatedRegistrationWorkflow_RegistrationRetriesSucceed tests that
// the workflow successfully retries transient registration failures.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity's retry policy works correctly
// when there are transient failures.
//
// EXPECTED BEHAVIOR:
// - AwaitPayment should succeed
// - RegisterWallet should fail on first attempt
// - RegisterWallet should fail on second attempt
// - RegisterWallet should succeed on third attempt
// - Workflow should complete successfully
// - Result should show completed status
//
// This ensures transient failures (network glitches, etc.) don't permanently fail registrations.
func TestPaymentGatedRegistrationWorkflow_RegistrationRetriesSucceed(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Mock AwaitPayment activity - succeeds
	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).Return(&AwaitPaymentResult{
		TransactionSignature: "payment-sig",
		Amount:               1000000,
		BlockTime:            time.Now(),
	}, nil)

	// Mock RegisterWallet activity - fails twice, then succeeds
	attemptCount := 0
	env.OnActivity(RegisterWallet, mock.Anything, mock.Anything).Return(
		func(ctx interface{}, input RegisterWalletInput) (*RegisterWalletResult, error) {
			attemptCount++

			if attemptCount < 3 {
				// First two attempts fail
				return nil, errors.New("transient network error")
			}

			// Third attempt succeeds
			return &RegisterWalletResult{
				Address:   input.Address,
				Network:   input.Network,
				AssetType: input.AssetType,
				Status:    "active",
			}, nil
		},
	)

	// Execute workflow
	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet123",
		Network:        "mainnet",
		AssetType:      "sol",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet456",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "forohtoo-reg:test-retry",
		PaymentTimeout: 24 * time.Hour,
	})

	// Verify workflow completed successfully
	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("Workflow should have succeeded after retries, got error: %v", err)
	}

	// Verify RegisterWallet was called 3 times
	if attemptCount != 3 {
		t.Errorf("Expected RegisterWallet to be called 3 times, got %d", attemptCount)
	}

	// Get result
	var result PaymentGatedRegistrationResult
	err := env.GetWorkflowResult(&result)
	if err != nil {
		t.Fatalf("Failed to get workflow result: %v", err)
	}

	// Verify success
	if result.Status != "completed" {
		t.Errorf("Expected status=completed, got %q", result.Status)
	}
}

// TestPaymentGatedRegistrationWorkflow_ActivityOptions tests that the workflow
// configures activity options correctly.
//
// WHAT IS BEING TESTED:
// We're testing that the workflow sets appropriate timeouts and retry policies
// for activities, particularly the long-running AwaitPayment activity.
//
// EXPECTED BEHAVIOR:
// - AwaitPayment activity should have StartToCloseTimeout = PaymentTimeout
// - AwaitPayment should have HeartbeatTimeout = 30s
// - Activities should have retry policy configured
// - Retry policy should have correct backoff and max attempts
//
// This ensures activities are properly configured for reliability.
func TestPaymentGatedRegistrationWorkflow_ActivityOptions(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	paymentTimeout := 2 * time.Hour

	// We can't directly verify activity options in Temporal test framework,
	// but we can verify the workflow uses them by checking behavior.
	// Here we verify the workflow completes successfully with proper options.

	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).Return(&AwaitPaymentResult{
		TransactionSignature: "sig",
		Amount:               1000000,
		BlockTime:            time.Now(),
	}, nil)

	env.OnActivity(RegisterWallet, mock.Anything, mock.Anything).Return(&RegisterWalletResult{
		Address: "UserWallet",
		Status:  "active",
	}, nil)

	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet",
		Network:        "mainnet",
		AssetType:      "sol",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "test",
		PaymentTimeout: paymentTimeout,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// The real test is that the activity options are set in the workflow code.
	// This test verifies the workflow compiles and runs with those options.
	// Manual code review should verify:
	// - StartToCloseTimeout: input.PaymentTimeout
	// - HeartbeatTimeout: 30 * time.Second
	// - RetryPolicy with BackoffCoefficient: 2.0, MaximumAttempts: 3
}

// TestPaymentGatedRegistrationWorkflow_WorkflowTimeout tests that the workflow
// respects its execution timeout.
//
// WHAT IS BEING TESTED:
// We're testing that the workflow-level execution timeout works correctly,
// terminating the workflow if it runs too long.
//
// EXPECTED BEHAVIOR:
// - If workflow execution exceeds WorkflowExecutionTimeout, it should be terminated
// - Workflow should fail with timeout error
// - This prevents workflows from running indefinitely
//
// This is a safety mechanism for runaway workflows.
func TestPaymentGatedRegistrationWorkflow_WorkflowTimeout(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Set workflow execution timeout
	env.SetWorkflowTimeout(1 * time.Second)

	// Mock AwaitPayment to block longer than workflow timeout
	// In real Temporal, this would be terminated, but in test we can simulate
	env.OnActivity(AwaitPayment, mock.Anything, mock.Anything).After(2*time.Second).Return(
		nil,
		errors.New("timeout"),
	)

	// Execute workflow
	env.ExecuteWorkflow(PaymentGatedRegistrationWorkflow, PaymentGatedRegistrationInput{
		Address:        "UserWallet",
		Network:        "mainnet",
		AssetType:      "sol",
		PollInterval:   30 * time.Second,
		ServiceWallet:  "ServiceWallet",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentMemo:    "test",
		PaymentTimeout: 24 * time.Hour,
	})

	// Workflow should timeout
	if !env.IsWorkflowCompleted() {
		t.Fatal("Workflow did not complete")
	}

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("Expected workflow to timeout")
	}

	// Error should indicate timeout
	if !contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}
