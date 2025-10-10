package temporal

import (
	"context"
	"time"
)

// Scheduler manages Temporal schedules for wallet polling.
// Each wallet gets its own schedule that triggers the PollWalletWorkflow.
type Scheduler interface {
	// CreateWalletSchedule creates a new schedule for polling a wallet.
	// The schedule will trigger the PollWalletWorkflow on the given interval.
	CreateWalletSchedule(ctx context.Context, address string, interval time.Duration) error

	// DeleteWalletSchedule deletes the schedule for a wallet.
	// This stops the wallet from being polled.
	DeleteWalletSchedule(ctx context.Context, address string) error
}

// scheduleID returns the Temporal schedule ID for a wallet address.
func scheduleID(address string) string {
	return "poll-wallet-" + address
}
