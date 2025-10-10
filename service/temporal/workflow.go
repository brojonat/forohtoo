package temporal

import (
	"fmt"
	"time"

	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PollWalletWorkflow is the Temporal workflow that polls a Solana wallet for new transactions.
// It is triggered by a Temporal schedule at a configured interval (e.g., every 30 seconds).
//
// The workflow performs these steps:
// 1. Poll Solana RPC for new transactions (PollSolana activity)
// 2. Write transactions to TimescaleDB (WriteTransactions activity)
// 3. Return summary of what was polled
//
// Note: Transaction publishing to NATS will be added in a future activity.
func PollWalletWorkflow(ctx workflow.Context, input PollWalletInput) (*PollWalletResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("PollWalletWorkflow started", "address", input.Address)

	result := &PollWalletResult{
		Address:  input.Address,
		PollTime: workflow.Now(ctx),
	}

	// Get the wallet's last known signature from workflow memo or search attributes
	// For now, we'll start by getting the most recent transactions
	// TODO: Track last signature in workflow state or query from DB
	var lastSignature *string

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Step 1: Poll Solana for new transactions
	logger.Debug("polling solana", "address", input.Address, "last_signature", lastSignature)

	pollInput := PollSolanaInput{
		Address:       input.Address,
		LastSignature: lastSignature,
		Limit:         100,
	}

	var pollResult *PollSolanaResult
	err := workflow.ExecuteActivity(ctx, "PollSolana", pollInput).Get(ctx, &pollResult)
	if err != nil {
		logger.Error("failed to poll solana", "address", input.Address, "error", err)
		errMsg := fmt.Sprintf("failed to poll solana: %v", err)
		result.Error = &errMsg
		return result, fmt.Errorf("failed to poll solana: %w", err)
	}

	logger.Info("polled solana successfully",
		"address", input.Address,
		"transaction_count", len(pollResult.Transactions),
		"newest_signature", pollResult.NewestSignature,
	)

	result.TransactionCount = len(pollResult.Transactions)
	result.NewestSignature = pollResult.NewestSignature
	result.OldestSignature = pollResult.OldestSignature

	// If no transactions found, we're done
	if len(pollResult.Transactions) == 0 {
		logger.Info("no new transactions found", "address", input.Address)
		return result, nil
	}

	// Step 2: Write transactions to database
	logger.Debug("writing transactions to database",
		"address", input.Address,
		"count", len(pollResult.Transactions),
	)

	writeInput := WriteTransactionsInput{
		WalletAddress: input.Address,
		Transactions:  pollResult.Transactions,
	}

	var writeResult *WriteTransactionsResult
	err = workflow.ExecuteActivity(ctx, "WriteTransactions", writeInput).Get(ctx, &writeResult)
	if err != nil {
		logger.Error("failed to write transactions",
			"address", input.Address,
			"error", err,
		)
		errMsg := fmt.Sprintf("failed to write transactions: %v", err)
		result.Error = &errMsg
		return result, fmt.Errorf("failed to write transactions: %w", err)
	}

	logger.Info("wrote transactions to database",
		"address", input.Address,
		"written", writeResult.Written,
		"skipped", writeResult.Skipped,
	)

	// Update result with newest signature for next poll
	result.LastSignatureSeen = pollResult.NewestSignature

	// TODO: Step 3: Publish transactions to NATS (future activity)
	// publishInput := PublishTransactionsInput{
	//     WalletAddress: input.Address,
	//     Transactions:  pollResult.Transactions,
	// }
	// err = workflow.ExecuteActivity(ctx, "PublishTransactions", publishInput).Get(ctx, nil)

	logger.Info("PollWalletWorkflow completed successfully",
		"address", input.Address,
		"transaction_count", result.TransactionCount,
		"written", writeResult.Written,
		"skipped", writeResult.Skipped,
	)

	return result, nil
}
