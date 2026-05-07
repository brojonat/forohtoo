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
	Address                string  `json:"address"`
	Network                string  `json:"network"`
	AssetType              string  `json:"asset_type"`
	TokenMint              string  `json:"token_mint"`
	AssociatedTokenAddress *string `json:"associated_token_address"`
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

	if a.forohtooClient == nil {
		return nil, fmt.Errorf("forohtoo client not configured in activities")
	}

	txn, err := a.forohtooClient.Await(ctx, input.PayToAddress, input.Network, input.LookbackPeriod, func(t *client.Transaction) bool {
		meetsAmount := t.Amount >= input.Amount
		matchesMemo := t.Memo != nil && *t.Memo == input.Memo
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

// RegisterWallet activity persists a wallet asset and adds the monitored
// address to the Helius webhook so its transactions begin streaming.
func (a *Activities) RegisterWallet(ctx context.Context, input RegisterWalletInput) (*RegisterWalletResult, error) {
	a.logger.InfoContext(ctx, "registering wallet",
		"address", input.Address,
		"network", input.Network,
		"asset_type", input.AssetType,
	)

	wallet, err := a.store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                input.Address,
		Network:                input.Network,
		AssetType:              input.AssetType,
		TokenMint:              input.TokenMint,
		AssociatedTokenAddress: input.AssociatedTokenAddress,
		Status:                 "active",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upsert wallet: %w", err)
	}

	if a.heliusClient == nil {
		// Roll back the upsert if there's no way to subscribe to the wallet.
		_ = a.store.DeleteWallet(ctx, input.Address, input.Network, input.AssetType, input.TokenMint)
		return nil, fmt.Errorf("helius client not configured in activities")
	}

	monitorAddr := input.Address
	if input.AssociatedTokenAddress != nil {
		monitorAddr = *input.AssociatedTokenAddress
	}
	if err := a.heliusClient.AddAddress(ctx, monitorAddr); err != nil {
		if delErr := a.store.DeleteWallet(ctx, input.Address, input.Network, input.AssetType, input.TokenMint); delErr != nil {
			a.logger.ErrorContext(ctx, "failed to roll back wallet after Helius error",
				"error", delErr,
				"address", input.Address,
			)
		}
		return nil, fmt.Errorf("failed to add address to Helius webhook: %w", err)
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
