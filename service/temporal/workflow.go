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

// USDCMainnetMintAddress is the SPL token mint address for USDC on mainnet.
// This must be set before the worker starts, typically during initialization.
var USDCMainnetMintAddress string

// USDCDevnetMintAddress is the SPL token mint address for USDC on devnet.
// This must be set before the worker starts, typically during initialization.
var USDCDevnetMintAddress string

// getUSDCAssociatedTokenAccount calculates the associated token account for USDC.
// Returns empty string if the wallet address is invalid or if the network's USDC mint address is not set.
func getUSDCAssociatedTokenAccount(walletAddress string, network string) string {
	// Select the correct USDC mint based on network
	var usdcMint string
	switch network {
	case "mainnet":
		usdcMint = USDCMainnetMintAddress
	case "devnet":
		usdcMint = USDCDevnetMintAddress
	default:
		return ""
	}

	if usdcMint == "" {
		return ""
	}

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
// 1. Get existing transaction signatures from the database (GetExistingTransactionSignatures activity)
// 2. Poll Solana RPC for new transactions (PollSolana activity)
// 3. Write transactions to TimescaleDB and publish to NATS (WriteTransactions activity)
// 4. Return summary of what was polled
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
		StartToCloseTimeout: 600 * time.Second, // 10 minutes (up from 5) to handle rate limits and retries
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
	// Get ALL existing signatures (no time filter) to ensure proper deduplication
	// This is safe because we're only fetching signatures, not full transaction data
	err := workflow.ExecuteActivity(ctx, a.GetExistingTransactionSignatures, GetExistingTransactionSignaturesInput{WalletAddress: input.Address, Network: input.Network, Since: nil}).Get(ctx, &existingSigsResult)
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
	// Limit reduced to 20 for public RPC compatibility
	// At 600ms per transaction, 20 txns = ~12 seconds fetch time
	mainWalletInput := PollSolanaInput{
		Address:            input.Address,
		Network:            input.Network,
		LastSignature:      lastSignature,
		Limit:              20,
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

	// Sleep to avoid rate limiting between main wallet and USDC ATA polls
	// At 600ms per transaction + retries, the main wallet poll just made ~20 RPC calls
	// Wait a bit before making another batch of calls
	workflow.Sleep(ctx, 2*time.Second)

	// Poll USDC associated token account
	usdcATA := getUSDCAssociatedTokenAccount(input.Address, input.Network)
	allTransactions := mainWalletResult.Transactions

	if usdcATA != "" {
		usdcATAInput := PollSolanaInput{
			Address:            usdcATA,
			Network:            input.Network,
			LastSignature:      lastSignature,
			Limit:              20, // Same limit as main wallet for public RPC
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
		Network:       input.Network,
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

	// Note: NATS publishing happens inside WriteTransactions activity (see activities.go)

	logger.Info("PollWalletWorkflow completed successfully",
		"address", input.Address,
		"transaction_count", result.TransactionCount,
		"written", writeResult.Written,
		"skipped", writeResult.Skipped,
	)

	return result, nil
}
