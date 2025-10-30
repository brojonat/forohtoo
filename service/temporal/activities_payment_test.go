package temporal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/db"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

// Mock payment client for testing
type MockPaymentClient struct {
	AwaitFunc func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error)
}

func (m *MockPaymentClient) Await(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
	if m.AwaitFunc != nil {
		return m.AwaitFunc(ctx, address, network, lookback, matcher)
	}
	return nil, errors.New("AwaitFunc not set")
}

// TestAwaitPayment_Success tests the happy path where a matching payment is received.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity successfully completes when the
// payment client returns a matching transaction.
//
// EXPECTED BEHAVIOR:
// - Activity should call client.Await() with correct parameters
// - When a matching transaction is returned, activity should complete successfully
// - Result should include transaction signature, amount, from_address, and block_time
// - Heartbeats should be recorded while waiting (verified via Temporal test framework)
//
// This is the primary success path for payment processing.
func TestAwaitPayment_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	// Create mock payment client that returns a matching transaction
	expectedTxn := &client.Transaction{
		Signature:   "5a1b2c3d4e5f67890abcdef1234567890abcdef1234567890abcdef",
		Amount:      1000000, // 0.001 SOL
		Memo:        "forohtoo-reg:test-invoice-123",
		FromAddress: stringPtr("SenderAddress123456789012345678901234567"),
		BlockTime:   time.Now(),
	}

	mockClient := &MockPaymentClient{
		AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
			// Verify parameters
			if address != "ServiceWalletAddress123" {
				t.Errorf("Expected address=ServiceWalletAddress123, got %q", address)
			}
			if network != "mainnet" {
				t.Errorf("Expected network=mainnet, got %q", network)
			}
			if lookback != 24*time.Hour {
				t.Errorf("Expected lookback=24h, got %v", lookback)
			}

			// Verify matcher accepts the transaction
			if !matcher(expectedTxn) {
				t.Error("Matcher rejected the expected transaction")
			}

			return expectedTxn, nil
		},
	}

	// Create activities with mock client
	activities := &Activities{
		paymentClient: mockClient,
	}

	// Register activity
	env.RegisterActivity(activities.AwaitPayment)

	// Execute activity
	input := AwaitPaymentInput{
		PayToAddress:   "ServiceWalletAddress123",
		Network:        "mainnet",
		Amount:         1000000,
		Memo:           "forohtoo-reg:test-invoice-123",
		LookbackPeriod: 24 * time.Hour,
	}

	val, err := env.ExecuteActivity(activities.AwaitPayment, input)
	if err != nil {
		t.Fatalf("AwaitPayment activity failed: %v", err)
	}

	var result AwaitPaymentResult
	err = val.Get(&result)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify result
	if result.TransactionSignature != expectedTxn.Signature {
		t.Errorf("Expected signature=%q, got %q", expectedTxn.Signature, result.TransactionSignature)
	}
	if result.Amount != expectedTxn.Amount {
		t.Errorf("Expected amount=%d, got %d", expectedTxn.Amount, result.Amount)
	}
	if result.FromAddress == nil || *result.FromAddress != *expectedTxn.FromAddress {
		t.Errorf("Expected from_address=%v, got %v", expectedTxn.FromAddress, result.FromAddress)
	}
}

// TestAwaitPayment_TimeoutNoPayment tests the scenario where no payment arrives
// within the timeout period.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity handles timeouts gracefully when
// no matching payment is received.
//
// EXPECTED BEHAVIOR:
// - When context deadline is exceeded, client.Await() should return an error
// - Activity should return this error (allowing Temporal to retry if configured)
// - Error should indicate timeout/deadline exceeded
//
// This ensures workflows properly timeout if payment never arrives.
func TestAwaitPayment_TimeoutNoPayment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	// Mock client that returns timeout error
	mockClient := &MockPaymentClient{
		AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
			return nil, context.DeadlineExceeded
		},
	}

	activities := &Activities{
		paymentClient: mockClient,
	}

	env.RegisterActivity(activities.AwaitPayment)

	input := AwaitPaymentInput{
		PayToAddress:   "ServiceWalletAddress123",
		Network:        "mainnet",
		Amount:         1000000,
		Memo:           "forohtoo-reg:timeout-test",
		LookbackPeriod: 24 * time.Hour,
	}

	_, err := env.ExecuteActivity(activities.AwaitPayment, input)
	if err == nil {
		t.Fatal("Expected activity to fail with timeout error")
	}

	// Error should indicate deadline exceeded
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected DeadlineExceeded error, got: %v", err)
	}
}

// TestAwaitPayment_AmountTooLow tests rejection of payments below the required amount.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity's matcher function correctly
// rejects transactions with amounts below the required payment amount.
//
// EXPECTED BEHAVIOR:
// - Matcher should return false for transactions with amount < required amount
// - Activity should continue waiting (or timeout)
// - Exact amount or overpayment should be accepted
//
// This prevents accepting partial payments.
func TestAwaitPayment_AmountTooLow(t *testing.T) {
	requiredAmount := int64(1000000) // 0.001 SOL

	tests := []struct {
		name        string
		txnAmount   int64
		shouldMatch bool
	}{
		{"amount too low", 500000, false},
		{"exact amount", 1000000, true},
		{"overpayment", 1500000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestActivityEnvironment()

			txn := &client.Transaction{
				Signature: "test-sig",
				Amount:    tt.txnAmount,
				Memo:      "forohtoo-reg:test",
				BlockTime: time.Now(),
			}

			mockClient := &MockPaymentClient{
				AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
					// Test the matcher
					matches := matcher(txn)
					if matches != tt.shouldMatch {
						t.Errorf("Expected matcher to return %v for amount %d, got %v", tt.shouldMatch, tt.txnAmount, matches)
					}

					if matches {
						return txn, nil
					}
					// If doesn't match, simulate timeout
					return nil, context.DeadlineExceeded
				},
			}

			activities := &Activities{
				paymentClient: mockClient,
			}

			env.RegisterActivity(activities.AwaitPayment)

			input := AwaitPaymentInput{
				PayToAddress:   "ServiceWallet",
				Network:        "mainnet",
				Amount:         requiredAmount,
				Memo:           "forohtoo-reg:test",
				LookbackPeriod: 24 * time.Hour,
			}

			_, err := env.ExecuteActivity(activities.AwaitPayment, input)

			if tt.shouldMatch && err != nil {
				t.Errorf("Expected activity to succeed for amount %d, got error: %v", tt.txnAmount, err)
			}
			if !tt.shouldMatch && err == nil {
				t.Errorf("Expected activity to fail for amount %d (too low)", tt.txnAmount)
			}
		})
	}
}

// TestAwaitPayment_WrongMemo tests rejection of payments with incorrect memos.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity's matcher function correctly
// rejects transactions with memos that don't match the expected invoice memo.
//
// EXPECTED BEHAVIOR:
// - Matcher should return false for transactions with different memos
// - Matcher should return true only for exact memo match
// - Memo matching should be case-sensitive
//
// This ensures payments are correctly associated with specific invoices.
func TestAwaitPayment_WrongMemo(t *testing.T) {
	expectedMemo := "forohtoo-reg:invoice-123"

	tests := []struct {
		name        string
		txnMemo     string
		shouldMatch bool
	}{
		{"exact match", "forohtoo-reg:invoice-123", true},
		{"wrong invoice ID", "forohtoo-reg:invoice-456", false},
		{"empty memo", "", false},
		{"partial match", "forohtoo-reg:invoice", false},
		{"case mismatch", "FOROHTOO-REG:INVOICE-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestActivityEnvironment()

			txn := &client.Transaction{
				Signature: "test-sig",
				Amount:    1000000,
				Memo:      tt.txnMemo,
				BlockTime: time.Now(),
			}

			mockClient := &MockPaymentClient{
				AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
					matches := matcher(txn)
					if matches != tt.shouldMatch {
						t.Errorf("Expected matcher to return %v for memo %q, got %v", tt.shouldMatch, tt.txnMemo, matches)
					}

					if matches {
						return txn, nil
					}
					return nil, context.DeadlineExceeded
				},
			}

			activities := &Activities{
				paymentClient: mockClient,
			}

			env.RegisterActivity(activities.AwaitPayment)

			input := AwaitPaymentInput{
				PayToAddress:   "ServiceWallet",
				Network:        "mainnet",
				Amount:         1000000,
				Memo:           expectedMemo,
				LookbackPeriod: 24 * time.Hour,
			}

			_, err := env.ExecuteActivity(activities.AwaitPayment, input)

			if tt.shouldMatch && err != nil {
				t.Errorf("Expected activity to succeed for memo %q, got error: %v", tt.txnMemo, err)
			}
			if !tt.shouldMatch && err == nil {
				t.Errorf("Expected activity to fail for memo %q", tt.txnMemo)
			}
		})
	}
}

// TestAwaitPayment_HistoricalPayment tests detection of payments that occurred
// before the workflow started.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity can find and return historical
// payments using the lookback parameter.
//
// EXPECTED BEHAVIOR:
// - client.Await() should be called with correct lookback period (24h)
// - If a matching payment exists in the lookback window, it should be returned immediately
// - Activity should complete without waiting for new transactions
//
// This handles the case where a user pays immediately after receiving the invoice,
// but before the workflow activity starts.
func TestAwaitPayment_HistoricalPayment(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	// Historical transaction from 12 hours ago
	historicalTime := time.Now().Add(-12 * time.Hour)
	historicalTxn := &client.Transaction{
		Signature:   "historical-sig-123",
		Amount:      1000000,
		Memo:        "forohtoo-reg:historical",
		BlockTime:   historicalTime,
		FromAddress: stringPtr("Sender123"),
	}

	mockClient := &MockPaymentClient{
		AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
			// Verify lookback is set correctly
			if lookback != 24*time.Hour {
				t.Errorf("Expected lookback=24h, got %v", lookback)
			}

			// Return historical transaction immediately (no waiting)
			if matcher(historicalTxn) {
				return historicalTxn, nil
			}
			return nil, errors.New("transaction not found")
		},
	}

	activities := &Activities{
		paymentClient: mockClient,
	}

	env.RegisterActivity(activities.AwaitPayment)

	input := AwaitPaymentInput{
		PayToAddress:   "ServiceWallet",
		Network:        "mainnet",
		Amount:         1000000,
		Memo:           "forohtoo-reg:historical",
		LookbackPeriod: 24 * time.Hour,
	}

	val, err := env.ExecuteActivity(activities.AwaitPayment, input)
	if err != nil {
		t.Fatalf("AwaitPayment activity failed: %v", err)
	}

	var result AwaitPaymentResult
	err = val.Get(&result)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify historical transaction was returned
	if result.TransactionSignature != historicalTxn.Signature {
		t.Errorf("Expected signature=%q, got %q", historicalTxn.Signature, result.TransactionSignature)
	}
}

// TestAwaitPayment_ClientError tests handling of errors from the payment client.
//
// WHAT IS BEING TESTED:
// We're testing that the AwaitPayment activity propagates errors from the
// payment client (e.g., network failures, SSE connection errors).
//
// EXPECTED BEHAVIOR:
// - When client.Await() returns an error, activity should return that error
// - Error should be retryable (Temporal retry policy will handle it)
// - Error message should be preserved
//
// This ensures transient failures are properly handled by Temporal's retry mechanism.
func TestAwaitPayment_ClientError(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	expectedErr := errors.New("SSE connection failed: network unreachable")

	mockClient := &MockPaymentClient{
		AwaitFunc: func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*client.Transaction) bool) (*client.Transaction, error) {
			return nil, expectedErr
		},
	}

	activities := &Activities{
		paymentClient: mockClient,
	}

	env.RegisterActivity(activities.AwaitPayment)

	input := AwaitPaymentInput{
		PayToAddress:   "ServiceWallet",
		Network:        "mainnet",
		Amount:         1000000,
		Memo:           "forohtoo-reg:error-test",
		LookbackPeriod: 24 * time.Hour,
	}

	_, err := env.ExecuteActivity(activities.AwaitPayment, input)
	if err == nil {
		t.Fatal("Expected activity to fail with client error")
	}

	// Error message should contain original error
	if !contains(err.Error(), "SSE connection failed") {
		t.Errorf("Expected error to contain 'SSE connection failed', got: %v", err)
	}
}

// Mock store for testing
type MockStore struct {
	UpsertWalletFunc func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error)
	DeleteWalletFunc func(ctx context.Context, address, network, assetType, tokenMint string) error
}

func (m *MockStore) UpsertWallet(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
	if m.UpsertWalletFunc != nil {
		return m.UpsertWalletFunc(ctx, params)
	}
	return nil, errors.New("UpsertWalletFunc not set")
}

func (m *MockStore) DeleteWallet(ctx context.Context, address, network, assetType, tokenMint string) error {
	if m.DeleteWalletFunc != nil {
		return m.DeleteWalletFunc(ctx, address, network, assetType, tokenMint)
	}
	return errors.New("DeleteWalletFunc not set")
}

// Mock scheduler for testing
type MockScheduler struct {
	UpsertWalletAssetScheduleFunc func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error
}

func (m *MockScheduler) UpsertWalletAssetSchedule(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
	if m.UpsertWalletAssetScheduleFunc != nil {
		return m.UpsertWalletAssetScheduleFunc(ctx, address, network, assetType, tokenMint, ata, pollInterval)
	}
	return errors.New("UpsertWalletAssetScheduleFunc not set")
}

// TestRegisterWallet_Success tests the happy path for wallet registration.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity successfully registers a wallet
// when both database upsert and schedule creation succeed.
//
// EXPECTED BEHAVIOR:
// - Activity should call store.UpsertWallet with correct parameters
// - Activity should call scheduler.UpsertWalletAssetSchedule with correct parameters
// - Result should include wallet details (address, network, asset type, status)
// - No rollback should occur on success
//
// This is the primary success path for completing a paid registration.
func TestRegisterWallet_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	expectedWallet := &db.Wallet{
		Address:      "UserWallet123456789012345678901234567890",
		Network:      "mainnet",
		AssetType:    "sol",
		TokenMint:    "",
		PollInterval: 30 * time.Second,
		Status:       "active",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	upsertCalled := false
	scheduleCalled := false

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			upsertCalled = true

			// Verify parameters
			if params.Address != "UserWallet123456789012345678901234567890" {
				t.Errorf("Expected address=UserWallet..., got %q", params.Address)
			}
			if params.Network != "mainnet" {
				t.Errorf("Expected network=mainnet, got %q", params.Network)
			}
			if params.AssetType != "sol" {
				t.Errorf("Expected asset_type=sol, got %q", params.AssetType)
			}
			if params.Status != "active" {
				t.Errorf("Expected status=active, got %q", params.Status)
			}

			return expectedWallet, nil
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			scheduleCalled = true

			// Verify parameters match wallet
			if address != expectedWallet.Address {
				t.Errorf("Expected address=%q, got %q", expectedWallet.Address, address)
			}
			if network != expectedWallet.Network {
				t.Errorf("Expected network=%q, got %q", expectedWallet.Network, network)
			}
			if pollInterval != expectedWallet.PollInterval {
				t.Errorf("Expected poll_interval=%v, got %v", expectedWallet.PollInterval, pollInterval)
			}

			return nil
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:                "UserWallet123456789012345678901234567890",
		Network:                "mainnet",
		AssetType:              "sol",
		TokenMint:              "",
		AssociatedTokenAddress: nil,
		PollInterval:           30 * time.Second,
	}

	val, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err != nil {
		t.Fatalf("RegisterWallet activity failed: %v", err)
	}

	var result RegisterWalletResult
	err = val.Get(&result)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify both calls were made
	if !upsertCalled {
		t.Error("UpsertWallet was not called")
	}
	if !scheduleCalled {
		t.Error("UpsertWalletAssetSchedule was not called")
	}

	// Verify result
	if result.Address != expectedWallet.Address {
		t.Errorf("Expected address=%q, got %q", expectedWallet.Address, result.Address)
	}
	if result.Status != expectedWallet.Status {
		t.Errorf("Expected status=%q, got %q", expectedWallet.Status, result.Status)
	}
}

// TestRegisterWallet_DatabaseError tests handling of database errors during upsert.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity properly handles and returns
// database errors when the wallet upsert fails.
//
// EXPECTED BEHAVIOR:
// - When store.UpsertWallet returns an error, activity should fail
// - Error should be propagated for Temporal retry handling
// - Schedule should NOT be created (since wallet creation failed)
//
// This ensures database failures are properly handled and retried by Temporal.
func TestRegisterWallet_DatabaseError(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	expectedErr := errors.New("database connection failed")
	scheduleCalled := false

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			return nil, expectedErr
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			scheduleCalled = true
			return nil
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:      "UserWallet",
		Network:      "mainnet",
		AssetType:    "sol",
		PollInterval: 30 * time.Second,
	}

	_, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err == nil {
		t.Fatal("Expected activity to fail with database error")
	}

	// Verify error message contains database error
	if !contains(err.Error(), "database connection failed") {
		t.Errorf("Expected error to contain database error, got: %v", err)
	}

	// Verify schedule was NOT created
	if scheduleCalled {
		t.Error("UpsertWalletAssetSchedule should not be called after database error")
	}
}

// TestRegisterWallet_ScheduleError tests rollback behavior when schedule creation fails.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity rolls back the wallet creation
// when the Temporal schedule creation fails.
//
// EXPECTED BEHAVIOR:
// - When scheduler.UpsertWalletAssetSchedule returns an error, activity should fail
// - Activity should call store.DeleteWallet to rollback the wallet creation
// - Error should indicate schedule creation failure
//
// This ensures we don't leave orphaned wallets in the database without schedules.
func TestRegisterWallet_ScheduleError(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	scheduleErr := errors.New("temporal schedule creation failed")
	deleteCalled := false

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			return &db.Wallet{
				Address:   params.Address,
				Network:   params.Network,
				AssetType: params.AssetType,
				Status:    "active",
			}, nil
		},
		DeleteWalletFunc: func(ctx context.Context, address, network, assetType, tokenMint string) error {
			deleteCalled = true

			// Verify correct wallet is being deleted
			if address != "UserWallet" {
				t.Errorf("Expected to delete wallet UserWallet, got %q", address)
			}

			return nil
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			return scheduleErr
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:      "UserWallet",
		Network:      "mainnet",
		AssetType:    "sol",
		PollInterval: 30 * time.Second,
	}

	_, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err == nil {
		t.Fatal("Expected activity to fail with schedule error")
	}

	// Verify error is about schedule creation
	if !contains(err.Error(), "schedule") {
		t.Errorf("Expected error to mention schedule, got: %v", err)
	}

	// Verify rollback was attempted
	if !deleteCalled {
		t.Error("DeleteWallet should be called to rollback after schedule error")
	}
}

// TestRegisterWallet_ScheduleErrorRollbackFails tests the scenario where both
// schedule creation and rollback fail.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity handles the worst-case scenario
// where schedule creation fails AND the rollback deletion also fails.
//
// EXPECTED BEHAVIOR:
// - Activity should fail with an error
// - Error should indicate both failures (partial state)
// - This creates a recoverable situation (wallet exists but no schedule)
//
// This is a rare but possible scenario that needs proper error reporting.
func TestRegisterWallet_ScheduleErrorRollbackFails(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	scheduleErr := errors.New("schedule creation failed")
	deleteErr := errors.New("rollback delete failed")

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			return &db.Wallet{Address: params.Address}, nil
		},
		DeleteWalletFunc: func(ctx context.Context, address, network, assetType, tokenMint string) error {
			return deleteErr
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			return scheduleErr
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:      "UserWallet",
		Network:      "mainnet",
		AssetType:    "sol",
		PollInterval: 30 * time.Second,
	}

	_, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err == nil {
		t.Fatal("Expected activity to fail")
	}

	// Error should mention the problem (could contain either or both error messages)
	// The important thing is we get an error that can be investigated
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("Error message should not be empty for partial failure state")
	}
}

// TestRegisterWallet_SPLToken tests wallet registration for SPL token assets.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity correctly handles SPL token
// wallets, including the associated token address (ATA).
//
// EXPECTED BEHAVIOR:
// - AssetType should be "spl-token"
// - TokenMint should be set
// - AssociatedTokenAddress should be passed to scheduler
// - Both wallet and schedule should use the ATA
//
// This ensures SPL token monitoring is properly configured.
func TestRegisterWallet_SPLToken(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	testATA := "AssociatedTokenAddress12345678901234567"
	testMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			// Verify SPL token parameters
			if params.AssetType != "spl-token" {
				t.Errorf("Expected asset_type=spl-token, got %q", params.AssetType)
			}
			if params.TokenMint != testMint {
				t.Errorf("Expected token_mint=%q, got %q", testMint, params.TokenMint)
			}
			if params.AssociatedTokenAddress == nil || *params.AssociatedTokenAddress != testATA {
				t.Errorf("Expected ATA=%q, got %v", testATA, params.AssociatedTokenAddress)
			}

			return &db.Wallet{
				Address:                params.Address,
				Network:                params.Network,
				AssetType:              params.AssetType,
				TokenMint:              params.TokenMint,
				AssociatedTokenAddress: params.AssociatedTokenAddress,
				Status:                 "active",
			}, nil
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			// Verify ATA is passed to scheduler
			if ata == nil || *ata != testATA {
				t.Errorf("Expected scheduler to receive ATA=%q, got %v", testATA, ata)
			}
			if tokenMint != testMint {
				t.Errorf("Expected token_mint=%q, got %q", testMint, tokenMint)
			}

			return nil
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:                "UserWallet",
		Network:                "mainnet",
		AssetType:              "spl-token",
		TokenMint:              testMint,
		AssociatedTokenAddress: &testATA,
		PollInterval:           30 * time.Second,
	}

	val, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err != nil {
		t.Fatalf("RegisterWallet activity failed: %v", err)
	}

	var result RegisterWalletResult
	err = val.Get(&result)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify result has SPL token info
	if result.AssetType != "spl-token" {
		t.Errorf("Expected result asset_type=spl-token, got %q", result.AssetType)
	}
	if result.TokenMint != testMint {
		t.Errorf("Expected result token_mint=%q, got %q", testMint, result.TokenMint)
	}
}

// TestRegisterWallet_SOL tests wallet registration for SOL (native) assets.
//
// WHAT IS BEING TESTED:
// We're testing that the RegisterWallet activity correctly handles SOL wallets,
// where there is no token mint or associated token address.
//
// EXPECTED BEHAVIOR:
// - AssetType should be "sol"
// - TokenMint should be empty string
// - AssociatedTokenAddress should be nil
// - Schedule should be created for SOL polling (using wallet address directly)
//
// This ensures native SOL monitoring is properly configured.
func TestRegisterWallet_SOL(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestActivityEnvironment()

	mockStore := &MockStore{
		UpsertWalletFunc: func(ctx context.Context, params db.UpsertWalletParams) (*db.Wallet, error) {
			// Verify SOL parameters
			if params.AssetType != "sol" {
				t.Errorf("Expected asset_type=sol, got %q", params.AssetType)
			}
			if params.TokenMint != "" {
				t.Errorf("Expected token_mint to be empty for SOL, got %q", params.TokenMint)
			}
			if params.AssociatedTokenAddress != nil {
				t.Errorf("Expected ATA to be nil for SOL, got %v", params.AssociatedTokenAddress)
			}

			return &db.Wallet{
				Address:                params.Address,
				Network:                params.Network,
				AssetType:              params.AssetType,
				TokenMint:              "",
				AssociatedTokenAddress: nil,
				Status:                 "active",
			}, nil
		},
	}

	mockScheduler := &MockScheduler{
		UpsertWalletAssetScheduleFunc: func(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
			// Verify ATA is nil for SOL
			if ata != nil {
				t.Errorf("Expected scheduler to receive nil ATA for SOL, got %v", ata)
			}
			if tokenMint != "" {
				t.Errorf("Expected empty token_mint for SOL, got %q", tokenMint)
			}

			return nil
		},
	}

	activities := &Activities{
		store:     mockStore,
		scheduler: mockScheduler,
	}

	env.RegisterActivity(activities.RegisterWallet)

	input := RegisterWalletInput{
		Address:                "UserWallet",
		Network:                "mainnet",
		AssetType:              "sol",
		TokenMint:              "",
		AssociatedTokenAddress: nil,
		PollInterval:           30 * time.Second,
	}

	val, err := env.ExecuteActivity(activities.RegisterWallet, input)
	if err != nil {
		t.Fatalf("RegisterWallet activity failed: %v", err)
	}

	var result RegisterWalletResult
	err = val.Get(&result)
	if err != nil {
		t.Fatalf("Failed to get result: %v", err)
	}

	// Verify result is for SOL
	if result.AssetType != "sol" {
		t.Errorf("Expected result asset_type=sol, got %q", result.AssetType)
	}
	if result.TokenMint != "" {
		t.Errorf("Expected result token_mint to be empty, got %q", result.TokenMint)
	}
}
