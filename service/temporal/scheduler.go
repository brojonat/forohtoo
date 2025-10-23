package temporal

import (
	"context"
	"time"
)

// Scheduler manages Temporal schedules for wallet polling.
// Each wallet+asset combination gets its own schedule that triggers the PollWalletWorkflow.
type Scheduler interface {
	// CreateWalletAssetSchedule creates a new schedule for polling a wallet asset.
	// The schedule will trigger the PollWalletWorkflow on the given interval.
	// For SOL assets, tokenMint should be empty and ata should be nil.
	// For SPL token assets, tokenMint and ata must be provided.
	CreateWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string, ata *string, interval time.Duration) error

	// DeleteWalletAssetSchedule deletes the schedule for a wallet asset.
	// This stops the wallet asset from being polled.
	DeleteWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string) error
}

// scheduleID returns the Temporal schedule ID for a wallet asset.
// Format: poll-wallet-{network}-{address}-{assetType}-{tokenMint}
// For SOL: poll-wallet-mainnet-ABC123...-sol-
// For USDC: poll-wallet-mainnet-ABC123...-spl-token-EPjF...
func scheduleID(address string, network string, assetType string, tokenMint string) string {
	return "poll-wallet-" + network + "-" + address + "-" + assetType + "-" + tokenMint
}
