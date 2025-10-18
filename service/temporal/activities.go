package temporal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/metrics"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/brojonat/forohtoo/service/solana"
	solanago "github.com/gagliardetto/solana-go"
)

// PollWalletInput contains the input parameters for polling a wallet.
type PollWalletInput struct {
	Address string `json:"address"`
}

// PollWalletResult contains the result of polling a wallet.
type PollWalletResult struct {
	Address           string    `json:"address"`
	TransactionCount  int       `json:"transaction_count"`
	NewestSignature   *string   `json:"newest_signature,omitempty"`
	OldestSignature   *string   `json:"oldest_signature,omitempty"`
	PollTime          time.Time `json:"poll_time"`
	LastSignatureSeen *string   `json:"last_signature_seen,omitempty"`
	Error             *string   `json:"error,omitempty"`
}

// PollSolanaInput contains parameters for the PollSolana activity.
type PollSolanaInput struct {
	Address            string   `json:"address"`
	LastSignature      *string  `json:"last_signature,omitempty"`
	Limit              int      `json:"limit"`
	ExistingSignatures []string `json:"existing_signatures"`
}

// PollSolanaResult contains the result of polling Solana.
type PollSolanaResult struct {
	Transactions    []*solana.Transaction `json:"transactions"`
	NewestSignature *string               `json:"newest_signature,omitempty"`
	OldestSignature *string               `json:"oldest_signature,omitempty"`
}

// WriteTransactionsInput contains parameters for the WriteTransactions activity.
type WriteTransactionsInput struct {
	WalletAddress string                `json:"wallet_address"`
	Transactions  []*solana.Transaction `json:"transactions"`
}

// WriteTransactionsResult contains the result of writing transactions.
type WriteTransactionsResult struct {
	Written int `json:"written"`
	Skipped int `json:"skipped"` // Already existed in DB
}

// GetExistingTransactionSignaturesInput contains parameters for the GetExistingTransactionSignatures activity.
type GetExistingTransactionSignaturesInput struct {
	WalletAddress string     `json:"wallet_address"`
	Since         *time.Time `json:"since,omitempty"`
}

// GetExistingTransactionSignaturesResult contains the result of the GetExistingTransactionSignatures activity.
type GetExistingTransactionSignaturesResult struct {
	Signatures []string `json:"signatures"`
}

// StoreInterface defines the database operations needed by activities.
// This allows for easy mocking in tests.
type StoreInterface interface {
	CreateTransaction(context.Context, db.CreateTransactionParams) (*db.Transaction, error)
	UpdateWalletPollTime(context.Context, string, time.Time) (*db.Wallet, error)
	GetTransaction(context.Context, string) (*db.Transaction, error)
	GetWallet(context.Context, string) (*db.Wallet, error)
	GetTransactionSignaturesByWallet(context.Context, string, *time.Time) ([]string, error)
}

// SolanaClientInterface defines the Solana operations needed by activities.
// This allows for easy mocking in tests.
type SolanaClientInterface interface {
	GetTransactionsSince(ctx context.Context, params solana.GetTransactionsSinceParams) ([]*solana.Transaction, error)
}

// PublisherInterface defines the NATS publishing operations needed by activities.
// This allows for easy mocking in tests.
type PublisherInterface interface {
	PublishTransaction(ctx context.Context, event *natspkg.TransactionEvent) error
	PublishTransactionBatch(ctx context.Context, events []*natspkg.TransactionEvent) error
}

// Activities holds the dependencies needed by Temporal activities.
// Following go-kit pattern, all dependencies are explicit.
type Activities struct {
	store        StoreInterface
	solanaClient SolanaClientInterface
	publisher    PublisherInterface
	metrics      *metrics.Metrics
	logger       *slog.Logger
}

// NewActivities creates a new Activities instance with explicit dependencies.
// If metrics is nil, no metrics will be recorded.
func NewActivities(
	store StoreInterface,
	solanaClient SolanaClientInterface,
	publisher PublisherInterface,
	m *metrics.Metrics,
	logger *slog.Logger,
) *Activities {
	if logger == nil {
		logger = slog.Default()
	}
	return &Activities{
		store:        store,
		solanaClient: solanaClient,
		publisher:    publisher,
		metrics:      m,
		logger:       logger,
	}
}

// PollSolana polls the Solana network for new transactions.
// This activity is responsible for fetching transactions from the Solana RPC.
func (a *Activities) PollSolana(ctx context.Context, input PollSolanaInput) (*PollSolanaResult, error) {
	start := time.Now()
	defer func() {
		if a.metrics != nil {
			a.logger.DebugContext(ctx, "recording activity duration metric", "activity", "PollSolana")
			a.metrics.RecordActivityDuration("PollSolana", input.Address, time.Since(start).Seconds())
		} else {
			a.logger.WarnContext(ctx, "metrics is nil, skipping metric recording", "activity", "PollSolana")
		}
	}()

	a.logger.DebugContext(ctx, "polling solana",
		"address", input.Address,
		"last_signature", input.LastSignature,
		"limit", input.Limit,
	)

	// Parse wallet address
	walletPubkey, err := solanago.PublicKeyFromBase58(input.Address)
	if err != nil {
		a.logger.ErrorContext(ctx, "invalid wallet address",
			"address", input.Address,
			"error", err,
		)
		return nil, fmt.Errorf("invalid wallet address: %w", err)
	}

	// Parse last signature if provided
	var lastSig *solanago.Signature
	if input.LastSignature != nil {
		sig, err := solanago.SignatureFromBase58(*input.LastSignature)
		if err != nil {
			a.logger.ErrorContext(ctx, "invalid last signature",
				"signature", *input.LastSignature,
				"error", err,
			)
			return nil, fmt.Errorf("invalid last signature: %w", err)
		}
		lastSig = &sig
	}

	// Set default limit if not provided
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}

	// Fetch transactions from Solana
	params := solana.GetTransactionsSinceParams{
		Wallet:             walletPubkey,
		LastSignature:      lastSig,
		Limit:              limit,
		ExistingSignatures: input.ExistingSignatures,
	}

	transactions, err := a.solanaClient.GetTransactionsSince(ctx, params)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to poll solana",
			"address", input.Address,
			"error", err,
		)
		return nil, fmt.Errorf("failed to poll solana: %w", err)
	}

	result := &PollSolanaResult{
		Transactions: transactions,
	}

	// Extract newest and oldest signatures
	if len(transactions) > 0 {
		// Transactions are in descending order (newest first)
		newest := transactions[0].Signature
		result.NewestSignature = &newest

		oldest := transactions[len(transactions)-1].Signature
		result.OldestSignature = &oldest
	}

	a.logger.InfoContext(ctx, "polled solana successfully",
		"address", input.Address,
		"count", len(transactions),
		"newest_signature", result.NewestSignature,
	)

	// Record transactions fetched metric
	if a.metrics != nil {
		// Determine source based on address (simplified - could be enhanced)
		source := "main_wallet"
		a.logger.DebugContext(ctx, "recording transactions fetched metric", "count", len(transactions))
		a.metrics.RecordTransactionsFetched(input.Address, source, len(transactions))
	} else {
		a.logger.WarnContext(ctx, "metrics is nil, skipping transactions fetched metric")
	}

	return result, nil
}

// GetExistingTransactionSignatures fetches existing transaction signatures from the database.
func (a *Activities) GetExistingTransactionSignatures(ctx context.Context, input GetExistingTransactionSignaturesInput) (*GetExistingTransactionSignaturesResult, error) {
	start := time.Now()
	defer func() {
		if a.metrics != nil {
			a.metrics.RecordActivityDuration("GetExistingSignatures", input.WalletAddress, time.Since(start).Seconds())
		}
	}()

	a.logger.DebugContext(ctx, "fetching existing transaction signatures",
		"wallet_address", input.WalletAddress,
		"since", input.Since,
	)

	signatures, err := a.store.GetTransactionSignaturesByWallet(ctx, input.WalletAddress, input.Since)
	if err != nil {
		a.logger.ErrorContext(ctx, "failed to get existing transaction signatures",
			"wallet_address", input.WalletAddress,
			"error", err,
		)
		return nil, fmt.Errorf("failed to get existing transaction signatures: %w", err)
	}

	result := &GetExistingTransactionSignaturesResult{
		Signatures: signatures,
	}

	a.logger.InfoContext(ctx, "fetched existing transaction signatures successfully",
		"wallet_address", input.WalletAddress,
		"count", len(signatures),
	)

	return result, nil
}

// WriteTransactions writes transactions to the database.
// This activity is responsible for persisting transactions to TimescaleDB.
// It handles duplicate transactions gracefully by skipping them.
// After writing, it publishes transaction events to NATS for real-time subscribers.
func (a *Activities) WriteTransactions(ctx context.Context, input WriteTransactionsInput) (*WriteTransactionsResult, error) {
	start := time.Now()
	defer func() {
		if a.metrics != nil {
			a.metrics.RecordActivityDuration("WriteTransactions", input.WalletAddress, time.Since(start).Seconds())
		}
	}()

	a.logger.DebugContext(ctx, "writing transactions",
		"wallet", input.WalletAddress,
		"count", len(input.Transactions),
	)

	written := 0
	skipped := 0
	var writtenTransactions []*db.Transaction

	for _, txn := range input.Transactions {
		// Convert solana.Transaction to db.CreateTransactionParams
		params := db.CreateTransactionParams{
			Signature:     txn.Signature,
			WalletAddress: input.WalletAddress,
			Slot:          int64(txn.Slot),
			BlockTime:     txn.BlockTime,
		}

		// Set amount (defaults to 0 if not present)
		params.Amount = int64(txn.Amount)

		// Set optional fields
		if txn.TokenMint != nil {
			params.TokenMint = txn.TokenMint
		}
		if txn.Memo != nil {
			params.Memo = txn.Memo
		}
		if txn.FromAddress != nil {
			params.FromAddress = txn.FromAddress
		}

		// Set confirmation status based on error
		if txn.Err != nil {
			status := "failed"
			params.ConfirmationStatus = status
		} else {
			status := "confirmed"
			params.ConfirmationStatus = status
		}

		// Try to create the transaction
		dbTxn, err := a.store.CreateTransaction(ctx, params)
		if err != nil {
			// Check if this is a duplicate key error (transaction already exists)
			// pgx returns errors with specific codes, but for now we'll check the error message
			if isDuplicateKeyError(err) {
				a.logger.DebugContext(ctx, "transaction already exists, skipping",
					"signature", txn.Signature,
				)
				skipped++
				continue
			}

			// Other errors are actual failures
			a.logger.ErrorContext(ctx, "failed to write transaction",
				"signature", txn.Signature,
				"error", err,
			)
			return nil, fmt.Errorf("failed to write transaction %s: %w", txn.Signature, err)
		}

		written++
		writtenTransactions = append(writtenTransactions, dbTxn)
	}

	// Update wallet's last poll time
	_, err := a.store.UpdateWalletPollTime(ctx, input.WalletAddress, time.Now())
	if err != nil {
		a.logger.WarnContext(ctx, "failed to update wallet last poll time",
			"wallet", input.WalletAddress,
			"error", err,
		)
		// Don't fail the activity for this - transactions are written
	}

	a.logger.InfoContext(ctx, "wrote transactions to database",
		"wallet", input.WalletAddress,
		"written", written,
		"skipped", skipped,
		"total", len(input.Transactions),
	)

	// Record transaction write metrics
	if a.metrics != nil {
		a.metrics.RecordTransactionsWritten(input.WalletAddress, written)
		a.metrics.RecordTransactionsSkipped(input.WalletAddress, "already_exists", skipped)

		// Calculate and record deduplication ratio
		total := float64(len(input.Transactions))
		if total > 0 {
			ratio := float64(skipped) / total
			a.metrics.RecordDeduplicationRatio(input.WalletAddress, ratio)
		}
	}

	// Publish newly written transactions to NATS
	if len(writtenTransactions) > 0 && a.publisher != nil {
		events := make([]*natspkg.TransactionEvent, 0, len(writtenTransactions))
		for _, txn := range writtenTransactions {
			events = append(events, natspkg.FromDBTransaction(txn))
		}

		if err := a.publisher.PublishTransactionBatch(ctx, events); err != nil {
			// Log error but don't fail the activity
			// Transactions are persisted, NATS publish is best-effort
			a.logger.ErrorContext(ctx, "failed to publish transactions to NATS",
				"wallet", input.WalletAddress,
				"count", len(events),
				"error", err,
			)
		} else {
			a.logger.DebugContext(ctx, "published transactions to NATS",
				"wallet", input.WalletAddress,
				"count", len(events),
			)
		}
	}

	return &WriteTransactionsResult{
		Written: written,
		Skipped: skipped,
	}, nil
}

// isDuplicateKeyError checks if an error is a duplicate key constraint violation.
// This happens when we try to insert a transaction that already exists.
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// PostgreSQL duplicate key error contains "duplicate key value violates unique constraint"
	return contains(err.Error(), "duplicate key value violates unique constraint") ||
		contains(err.Error(), "unique constraint") ||
		contains(err.Error(), "already exists")
}

// contains checks if a string contains a substring (case-insensitive helper).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(findSubstring(s, substr) != -1))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
