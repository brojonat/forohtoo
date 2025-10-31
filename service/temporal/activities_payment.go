package temporal

import (
	"context"
	"fmt"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/db"
	"go.temporal.io/sdk/activity"
)

// AwaitPaymentInput contains parameters for awaiting payment.
type AwaitPaymentInput struct {
	PayToAddress   string        `json:"pay_to_address"`
	Network        string        `json:"network"`
	Amount         int64         `json:"amount"`
	Memo           string        `json:"memo"`
	LookbackPeriod time.Duration `json:"lookback_period"`
}

// AwaitPaymentResult contains the result of awaiting payment.
type AwaitPaymentResult struct {
	TransactionSignature string    `json:"transaction_signature"`
	Amount               int64     `json:"amount"`
	FromAddress          *string   `json:"from_address,omitempty"`
	BlockTime            time.Time `json:"block_time"`
}

// RegisterWalletInput contains parameters for registering a wallet.
type RegisterWalletInput struct {
	Address                string        `json:"address"`
	Network                string        `json:"network"`
	AssetType              string        `json:"asset_type"`
	TokenMint              string        `json:"token_mint"`
	AssociatedTokenAddress *string       `json:"associated_token_address"`
	PollInterval           time.Duration `json:"poll_interval"`
}

// RegisterWalletResult contains the result of registering a wallet.
type RegisterWalletResult struct {
	Address   string `json:"address"`
	Network   string `json:"network"`
	AssetType string `json:"asset_type"`
	TokenMint string `json:"token_mint"`
	Status    string `json:"status"`
}

// AwaitPayment activity waits for a payment transaction to arrive.
// Uses the client library's Await() method to block until payment received.
func (a *Activities) AwaitPayment(ctx context.Context, input AwaitPaymentInput) (*AwaitPaymentResult, error) {
	a.logger.InfoContext(ctx, "waiting for payment",
		"address", input.PayToAddress,
		"network", input.Network,
		"amount", input.Amount,
		"memo", input.Memo,
	)

	// Send heartbeats while waiting (every 30s)
	// This lets Temporal know the activity is still alive
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx, "waiting for payment")
			}
		}
	}()

	// Use forohtoo client to await payment
	if a.forohtooClient == nil {
		return nil, fmt.Errorf("forohtoo client not configured in activities")
	}

	txn, err := a.forohtooClient.Await(ctx, input.PayToAddress, input.Network, input.LookbackPeriod, func(t *client.Transaction) bool {
		// Match on memo and minimum amount
		meetsAmount := t.Amount >= input.Amount
		matchesMemo := t.Memo != nil && *t.Memo == input.Memo

		memoValue := ""
		if t.Memo != nil {
			memoValue = *t.Memo
		}

		a.logger.DebugContext(ctx, "checking transaction",
			"signature", t.Signature,
			"amount", t.Amount,
			"required_amount", input.Amount,
			"meets_amount", meetsAmount,
			"memo", memoValue,
			"required_memo", input.Memo,
			"matches_memo", matchesMemo,
		)

		return meetsAmount && matchesMemo
	})

	if err != nil {
		return nil, fmt.Errorf("payment await failed: %w", err)
	}

	a.logger.InfoContext(ctx, "payment received",
		"txn_signature", txn.Signature,
		"amount", txn.Amount,
		"from", txn.FromAddress,
	)

	return &AwaitPaymentResult{
		TransactionSignature: txn.Signature,
		Amount:               txn.Amount,
		FromAddress:          txn.FromAddress,
		BlockTime:            txn.BlockTime,
	}, nil
}

// RegisterWallet activity registers a wallet asset and creates its Temporal schedule.
func (a *Activities) RegisterWallet(ctx context.Context, input RegisterWalletInput) (*RegisterWalletResult, error) {
	a.logger.InfoContext(ctx, "registering wallet",
		"address", input.Address,
		"network", input.Network,
		"asset_type", input.AssetType,
	)

	// Upsert wallet in database
	wallet, err := a.store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                input.Address,
		Network:                input.Network,
		AssetType:              input.AssetType,
		TokenMint:              input.TokenMint,
		AssociatedTokenAddress: input.AssociatedTokenAddress,
		PollInterval:           input.PollInterval,
		Status:                 "active",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert wallet: %w", err)
	}

	// Create Temporal schedule for polling
	if a.temporalClient == nil {
		// Rollback wallet creation
		a.store.DeleteWallet(ctx, input.Address, input.Network, input.AssetType, input.TokenMint)
		return nil, fmt.Errorf("temporal client not configured in activities")
	}

	err = a.temporalClient.UpsertWalletAssetSchedule(ctx,
		input.Address,
		input.Network,
		input.AssetType,
		input.TokenMint,
		input.AssociatedTokenAddress,
		input.PollInterval,
	)
	if err != nil {
		// Rollback wallet creation
		deleteErr := a.store.DeleteWallet(ctx, input.Address, input.Network, input.AssetType, input.TokenMint)
		if deleteErr != nil {
			a.logger.ErrorContext(ctx, "failed to rollback wallet creation after schedule error",
				"error", deleteErr,
				"address", input.Address,
			)
		}
		return nil, fmt.Errorf("failed to create schedule: %w", err)
	}

	a.logger.InfoContext(ctx, "wallet registered successfully",
		"address", input.Address,
		"network", input.Network,
	)

	return &RegisterWalletResult{
		Address:   wallet.Address,
		Network:   wallet.Network,
		AssetType: wallet.AssetType,
		TokenMint: wallet.TokenMint,
		Status:    wallet.Status,
	}, nil
}
