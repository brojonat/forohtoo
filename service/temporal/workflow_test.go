package temporal

import (
	"errors"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/solana"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
)

func TestPollWalletWorkflow(t *testing.T) {
	testWallet := "TestWa11et11111111111111111111111111111"

	tests := []struct {
		name             string
		input            PollWalletInput
		mockActivities   func(*testsuite.MockCallWrapper, *testsuite.MockCallWrapper, *testsuite.MockCallWrapper)
		expectedError    bool
		validateResult   func(*testing.T, *PollWalletResult)
	}{
		{
			name: "successful workflow with transactions",
			input: PollWalletInput{
				Address: testWallet,
			},
			mockActivities: func(pollMock, writeMock *testsuite.MockCallWrapper, getSigsMock *testsuite.MockCallWrapper) {
				// Mock GetExistingTransactionSignatures activity
				getSigsResult := &GetExistingTransactionSignaturesResult{
					Signatures: []string{}, // Empty - no existing signatures
				}
				getSigsMock.Return(getSigsResult, nil)

				// Mock PollSolana activity
				pollResult := &PollSolanaResult{
					Transactions: []*solana.Transaction{
						{
							Signature: "sig1",
							Slot:      1000,
							BlockTime: time.Now(),
							Amount:    100,
						},
						{
							Signature: "sig2",
							Slot:      999,
							BlockTime: time.Now().Add(-time.Minute),
							Amount:    200,
						},
					},
					NewestSignature: stringPtr("sig1"),
					OldestSignature: stringPtr("sig2"),
				}
				pollMock.Return(pollResult, nil)

				// Mock WriteTransactions activity
				writeResult := &WriteTransactionsResult{
					Written: 2,
					Skipped: 0,
				}
				writeMock.Return(writeResult, nil)
			},
			expectedError: false,
			validateResult: func(t *testing.T, result *PollWalletResult) {
				assert.Equal(t, testWallet, result.Address)
				assert.Equal(t, 2, result.TransactionCount)
				assert.NotNil(t, result.NewestSignature)
				assert.Equal(t, "sig1", *result.NewestSignature)
				assert.NotNil(t, result.OldestSignature)
				assert.Equal(t, "sig2", *result.OldestSignature)
				assert.Nil(t, result.Error)
			},
		},
		{
			name: "successful workflow with no transactions",
			input: PollWalletInput{
				Address: testWallet,
			},
			mockActivities: func(pollMock, writeMock *testsuite.MockCallWrapper, getSigsMock *testsuite.MockCallWrapper) {
				// Mock GetExistingTransactionSignatures activity
				getSigsResult := &GetExistingTransactionSignaturesResult{
					Signatures: []string{},
				}
				getSigsMock.Return(getSigsResult, nil)

				// Mock PollSolana activity - returns empty list
				pollResult := &PollSolanaResult{
					Transactions:    []*solana.Transaction{},
					NewestSignature: nil,
					OldestSignature: nil,
				}
				pollMock.Return(pollResult, nil)

				// WriteTransactions should NOT be called when there are no transactions
			},
			expectedError: false,
			validateResult: func(t *testing.T, result *PollWalletResult) {
				assert.Equal(t, testWallet, result.Address)
				assert.Equal(t, 0, result.TransactionCount)
				assert.Nil(t, result.NewestSignature)
				assert.Nil(t, result.OldestSignature)
				assert.Nil(t, result.Error)
			},
		},
		{
			name: "poll solana fails",
			input: PollWalletInput{
				Address: testWallet,
			},
			mockActivities: func(pollMock, writeMock *testsuite.MockCallWrapper, getSigsMock *testsuite.MockCallWrapper) {
				// Mock GetExistingTransactionSignatures activity
				getSigsResult := &GetExistingTransactionSignaturesResult{
					Signatures: []string{},
				}
				getSigsMock.Return(getSigsResult, nil)

				// Mock PollSolana activity failure
				pollMock.Return(nil, errors.New("solana RPC error"))

				// WriteTransactions should NOT be called
			},
			expectedError: true,
			validateResult: func(t *testing.T, result *PollWalletResult) {
				// When workflow errors, the result might be partially populated or empty
				// Just verify the workflow failed - the error is checked separately
			},
		},
		{
			name: "write transactions fails",
			input: PollWalletInput{
				Address: testWallet,
			},
			mockActivities: func(pollMock, writeMock *testsuite.MockCallWrapper, getSigsMock *testsuite.MockCallWrapper) {
				// Mock GetExistingTransactionSignatures activity
				getSigsResult := &GetExistingTransactionSignaturesResult{
					Signatures: []string{},
				}
				getSigsMock.Return(getSigsResult, nil)

				// Mock PollSolana activity success
				pollResult := &PollSolanaResult{
					Transactions: []*solana.Transaction{
						{
							Signature: "sig1",
							Slot:      1000,
							BlockTime: time.Now(),
							Amount:    100,
						},
					},
					NewestSignature: stringPtr("sig1"),
					OldestSignature: stringPtr("sig1"),
				}
				pollMock.Return(pollResult, nil)

				// Mock WriteTransactions activity failure
				writeMock.Return(nil, errors.New("database error"))
			},
			expectedError: true,
			validateResult: func(t *testing.T, result *PollWalletResult) {
				// When workflow errors, the result might be partially populated
				// The workflow records what it can before failing
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestWorkflowEnvironment()

			// Register activities first (before mocking)
			activities := &Activities{}
			env.RegisterActivity(activities.GetExistingTransactionSignatures)
			env.RegisterActivity(activities.PollSolana)
			env.RegisterActivity(activities.WriteTransactions)

			// Mock activities
			getSigsMock := env.OnActivity(activities.GetExistingTransactionSignatures, mock.Anything, mock.Anything)
			pollMock := env.OnActivity(activities.PollSolana, mock.Anything, mock.Anything)
			writeMock := env.OnActivity(activities.WriteTransactions, mock.Anything, mock.Anything)

			tt.mockActivities(pollMock, writeMock, getSigsMock)

			// Execute workflow
			env.ExecuteWorkflow(PollWalletWorkflow, tt.input)

			// Check for errors
			if tt.expectedError {
				assert.Error(t, env.GetWorkflowError())

				// For workflows that error, the result might not be fully populated
				// We need to extract what we can from the workflow error
				var result PollWalletResult
				env.GetWorkflowResult(&result)
				tt.validateResult(t, &result)
			} else {
				assert.NoError(t, env.GetWorkflowError())

				// Validate result
				var result PollWalletResult
				err := env.GetWorkflowResult(&result)
				assert.NoError(t, err)
				tt.validateResult(t, &result)
			}
		})
	}
}

func TestPollWalletWorkflow_ActivityRetries(t *testing.T) {
	testWallet := "TestWa11et11111111111111111111111111111"

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register activities first
	activities := &Activities{}
	env.RegisterActivity(activities.GetExistingTransactionSignatures)
	env.RegisterActivity(activities.PollSolana)
	env.RegisterActivity(activities.WriteTransactions)

	// Mock GetExistingTransactionSignatures to succeed
	env.OnActivity(activities.GetExistingTransactionSignatures, mock.Anything, mock.Anything).
		Return(&GetExistingTransactionSignaturesResult{
			Signatures: []string{},
		}, nil)

	// Mock PollSolana to fail twice then succeed
	callCount := 0
	env.OnActivity(activities.PollSolana, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		callCount++
		if callCount < 3 {
			panic("transient error") // Temporal retries on panics
		}
	}).Return(&PollSolanaResult{
		Transactions: []*solana.Transaction{
			{
				Signature: "sig1",
				Slot:      1000,
				BlockTime: time.Now(),
				Amount:    100,
			},
		},
		NewestSignature: stringPtr("sig1"),
		OldestSignature: stringPtr("sig1"),
	}, nil)

	// Mock WriteTransactions to succeed
	env.OnActivity(activities.WriteTransactions, mock.Anything, mock.Anything).
		Return(&WriteTransactionsResult{
			Written: 1,
			Skipped: 0,
		}, nil)

	// Execute workflow
	env.ExecuteWorkflow(PollWalletWorkflow, PollWalletInput{Address: testWallet})

	// Workflow should succeed after retries
	assert.NoError(t, env.GetWorkflowError())

	var result PollWalletResult
	err := env.GetWorkflowResult(&result)
	assert.NoError(t, err)
	assert.Equal(t, 1, result.TransactionCount)

	// Verify PollSolana was called 3 times (2 failures + 1 success)
	assert.Equal(t, 3, callCount)
}

func TestPollWalletWorkflow_WorkflowTimer(t *testing.T) {
	testWallet := "TestWa11et11111111111111111111111111111"

	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()

	// Register activities first
	activities := &Activities{}
	env.RegisterActivity(activities.GetExistingTransactionSignatures)
	env.RegisterActivity(activities.PollSolana)
	env.RegisterActivity(activities.WriteTransactions)

	// Record workflow start time
	startTime := env.Now()

	// Mock activities
	env.OnActivity(activities.GetExistingTransactionSignatures, mock.Anything, mock.Anything).
		Return(&GetExistingTransactionSignaturesResult{
			Signatures: []string{},
		}, nil)

	env.OnActivity(activities.PollSolana, mock.Anything, mock.Anything).
		Return(&PollSolanaResult{
			Transactions:    []*solana.Transaction{},
			NewestSignature: nil,
			OldestSignature: nil,
		}, nil)

	// Execute workflow
	env.ExecuteWorkflow(PollWalletWorkflow, PollWalletInput{Address: testWallet})

	// Check workflow completed quickly (activities have timeouts but no explicit sleep)
	endTime := env.Now()
	duration := endTime.Sub(startTime)

	// Workflow should complete in less than activity timeout (30s)
	assert.Less(t, duration, 30*time.Second)
	assert.NoError(t, env.GetWorkflowError())
}
