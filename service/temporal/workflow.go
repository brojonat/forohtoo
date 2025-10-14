package temporal

import (
	"fmt"
	"time"

	"github.com/brojonat/forohtoo/service/solana"
	solanago "github.com/gagliardetto/solana-go"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var a *Activities // for type-safe activity invocation

// getUSDCAssociatedTokenAccount calculates the associated token account for USDC.
// Returns empty string if the wallet address is invalid.
func getUSDCAssociatedTokenAccount(walletAddress string) string {
	const usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	wallet, err := solanago.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return ""
	}

	mint, err := solanago.PublicKeyFromBase58(usdcMint)
	if err != nil {
		return ""
	}

	ata, _, err := solanago.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return ""
	}

	return ata.String()
}

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
		StartToCloseTimeout: 300 * time.Second,
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Step 1: Get existing transaction signatures from the database
	var existingSigsResult *GetExistingTransactionSignaturesResult
	since := workflow.Now(ctx).Add(-24 * time.Hour)
	err := workflow.ExecuteActivity(ctx, a.GetExistingTransactionSignatures, GetExistingTransactionSignaturesInput{WalletAddress: input.Address, Since: &since}).Get(ctx, &existingSigsResult)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get existing transaction signatures: %v", err)
		result.Error = &errMsg
		return result, fmt.Errorf("failed to get existing transaction signatures: %w", err)
	}
	logger.Info("got existing transaction signatures", "count", len(existingSigsResult.Signatures))

	// Step 2: Poll Solana for new transactions
	// Query both the main wallet address AND the USDC associated token account
	// to capture all relevant transactions (SOL transfers and USDC transfers)
	logger.Debug("polling solana", "address", input.Address, "last_signature", lastSignature)

	// Poll main wallet address
	mainWalletInput := PollSolanaInput{
		Address:            input.Address,
		LastSignature:      lastSignature,
		Limit:              1000,
		ExistingSignatures: existingSigsResult.Signatures,
	}

	var mainWalletResult *PollSolanaResult
	err = workflow.ExecuteActivity(ctx, a.PollSolana, mainWalletInput).Get(ctx, &mainWalletResult)
	if err != nil {
		logger.Error("failed to poll main wallet", "address", input.Address, "error", err)
		errMsg := fmt.Sprintf("failed to poll main wallet: %v", err)
		result.Error = &errMsg
		return result, fmt.Errorf("failed to poll main wallet: %w", err)
	}

	logger.Info("polled main wallet successfully",
		"address", input.Address,
		"transaction_count", len(mainWalletResult.Transactions),
	)

	// Poll USDC associated token account
	usdcATA := getUSDCAssociatedTokenAccount(input.Address)
	allTransactions := mainWalletResult.Transactions

	if usdcATA != "" {
		usdcATAInput := PollSolanaInput{
			Address:            usdcATA,
			LastSignature:      lastSignature,
			Limit:              1000,
			ExistingSignatures: existingSigsResult.Signatures,
		}

		var usdcATAResult *PollSolanaResult
		err = workflow.ExecuteActivity(ctx, a.PollSolana, usdcATAInput).Get(ctx, &usdcATAResult)
		if err != nil {
			// Log error but don't fail the workflow - continue with main wallet transactions
			logger.Warn("failed to poll USDC ATA", "ata", usdcATA, "error", err)
		} else {
			logger.Info("polled USDC ATA successfully",
				"ata", usdcATA,
				"transaction_count", len(usdcATAResult.Transactions),
			)

			// Merge transactions and deduplicate by signature
			seenSignatures := make(map[string]bool)
			for _, txn := range mainWalletResult.Transactions {
				seenSignatures[txn.Signature] = true
			}

			for _, txn := range usdcATAResult.Transactions {
				if !seenSignatures[txn.Signature] {
					allTransactions = append(allTransactions, txn)
					seenSignatures[txn.Signature] = true
				}
			}

			logger.Info("merged transactions from both addresses",
				"main_wallet_count", len(mainWalletResult.Transactions),
				"usdc_ata_count", len(usdcATAResult.Transactions),
				"total_unique_count", len(allTransactions),
			)
		}
	}

	// Create merged result
	pollResult := &PollSolanaResult{
		Transactions: allTransactions,
	}

	// Extract newest and oldest signatures from merged transactions
	if len(pollResult.Transactions) > 0 {
		// Find newest signature (highest slot)
		var newestTxn *solana.Transaction
		var oldestTxn *solana.Transaction

		for _, txn := range pollResult.Transactions {
			if newestTxn == nil || txn.Slot > newestTxn.Slot {
				newestTxn = txn
			}
			if oldestTxn == nil || txn.Slot < oldestTxn.Slot {
				oldestTxn = txn
			}
		}

		if newestTxn != nil {
			pollResult.NewestSignature = &newestTxn.Signature
		}
		if oldestTxn != nil {
			pollResult.OldestSignature = &oldestTxn.Signature
		}
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

	// Step 3: Write transactions to database
	logger.Debug("writing transactions to database",
		"address", input.Address,
		"count", len(pollResult.Transactions),
	)

	writeInput := WriteTransactionsInput{
		WalletAddress: input.Address,
		Transactions:  pollResult.Transactions,
	}

	var writeResult *WriteTransactionsResult
	err = workflow.ExecuteActivity(ctx, a.WriteTransactions, writeInput).Get(ctx, &writeResult)
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

	// FIXME:TODO: Step 4: Publish transactions to NATS (future activity)
	// publishInput := PublishTransactionsInput{
	//     WalletAddress: input.Address,
	//     Transactions:  pollResult.Transactions,
	// }
	// err = workflow.ExecuteActivity(ctx, a.PublishTransactions, publishInput).Get(ctx, nil)

	logger.Info("PollWalletWorkflow completed successfully",
		"address", input.Address,
		"transaction_count", result.TransactionCount,
		"written", writeResult.Written,
		"skipped", writeResult.Skipped,
	)

	return result, nil
}
