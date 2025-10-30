package temporal

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// PaymentGatedRegistrationInput contains input for payment-gated registration.
type PaymentGatedRegistrationInput struct {
	// Wallet to register
	Address                string        `json:"address"`
	Network                string        `json:"network"`
	AssetType              string        `json:"asset_type"`
	TokenMint              string        `json:"token_mint"`
	AssociatedTokenAddress *string       `json:"associated_token_address"`
	PollInterval           time.Duration `json:"poll_interval"`

	// Payment details
	ServiceWallet  string        `json:"service_wallet"`  // Forohtoo's wallet
	ServiceNetwork string        `json:"service_network"` // Where to monitor payment
	FeeAmount      int64         `json:"fee_amount"`
	PaymentMemo    string        `json:"payment_memo"`
	PaymentTimeout time.Duration `json:"payment_timeout"`
}

// PaymentGatedRegistrationResult contains the result of payment-gated registration.
type PaymentGatedRegistrationResult struct {
	Address          string    `json:"address"`
	Network          string    `json:"network"`
	AssetType        string    `json:"asset_type"`
	TokenMint        string    `json:"token_mint"`
	PaymentSignature *string   `json:"payment_signature,omitempty"`
	PaymentAmount    int64     `json:"payment_amount"`
	RegisteredAt     time.Time `json:"registered_at"`
	Status           string    `json:"status"` // "pending", "completed", "failed"
	Error            *string   `json:"error,omitempty"`
}

// PaymentGatedRegistrationWorkflow handles wallet registration with payment gating.
// This workflow:
// 1. Waits for payment via AwaitPayment activity (uses client.Await)
// 2. Registers the wallet and creates Temporal schedule
// 3. Returns registration confirmation
func PaymentGatedRegistrationWorkflow(ctx workflow.Context, input PaymentGatedRegistrationInput) (*PaymentGatedRegistrationResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("PaymentGatedRegistrationWorkflow started",
		"address", input.Address,
		"network", input.Network,
		"asset_type", input.AssetType,
	)

	result := &PaymentGatedRegistrationResult{
		Address:   input.Address,
		Network:   input.Network,
		AssetType: input.AssetType,
		TokenMint: input.TokenMint,
	}

	// Configure activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: input.PaymentTimeout, // Long timeout for payment wait
		HeartbeatTimeout:    30 * time.Second,     // Heartbeat every 30s while waiting
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Step 1: Await payment
	awaitInput := AwaitPaymentInput{
		PayToAddress:   input.ServiceWallet,
		Network:        input.ServiceNetwork,
		Amount:         input.FeeAmount,
		Memo:           input.PaymentMemo,
		LookbackPeriod: 24 * time.Hour, // Check last 24h in case payment came before workflow started
	}

	var awaitResult *AwaitPaymentResult
	err := workflow.ExecuteActivity(ctx, "AwaitPayment", awaitInput).Get(ctx, &awaitResult)
	if err != nil {
		logger.Error("payment await failed", "error", err)
		errMsg := fmt.Sprintf("payment await failed: %v", err)
		result.Error = &errMsg
		result.Status = "failed"
		return result, fmt.Errorf("payment await failed: %w", err)
	}

	logger.Info("payment received",
		"txn_signature", awaitResult.TransactionSignature,
		"amount", awaitResult.Amount,
	)

	result.PaymentSignature = &awaitResult.TransactionSignature
	result.PaymentAmount = awaitResult.Amount

	// Step 2: Register wallet
	registerInput := RegisterWalletInput{
		Address:                input.Address,
		Network:                input.Network,
		AssetType:              input.AssetType,
		TokenMint:              input.TokenMint,
		AssociatedTokenAddress: input.AssociatedTokenAddress,
		PollInterval:           input.PollInterval,
	}

	var registerResult *RegisterWalletResult
	err = workflow.ExecuteActivity(ctx, "RegisterWallet", registerInput).Get(ctx, &registerResult)
	if err != nil {
		logger.Error("wallet registration failed", "error", err)
		errMsg := fmt.Sprintf("wallet registration failed: %v", err)
		result.Error = &errMsg
		result.Status = "failed"
		return result, fmt.Errorf("wallet registration failed: %w", err)
	}

	logger.Info("wallet registered successfully",
		"address", input.Address,
		"network", input.Network,
		"asset_type", input.AssetType,
	)

	result.RegisteredAt = workflow.Now(ctx)
	result.Status = "completed"

	return result, nil
}
