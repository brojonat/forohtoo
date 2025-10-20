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
	CreateWalletSchedule(ctx context.Context, address string, network string, interval time.Duration) error

	// DeleteWalletSchedule deletes the schedule for a wallet.
	// This stops the wallet from being polled.
	DeleteWalletSchedule(ctx context.Context, address string, network string) error
}

// scheduleID returns the Temporal schedule ID for a wallet address and network.
// Network is included to allow same wallet on different networks.
func scheduleID(address string, network string) string {
	return "poll-wallet-" + network + "-" + address
}
