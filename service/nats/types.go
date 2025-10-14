package nats

import (
	"time"

	"github.com/brojonat/forohtoo/service/db"
)

// TransactionEvent represents a transaction event published to NATS.
// This is published to the subject "txns.{wallet_address}" in JetStream.
type TransactionEvent struct {
	// Transaction identifiers
	Signature string `json:"signature"`
	Slot      int64  `json:"slot"`

	// Wallet information
	WalletAddress string  `json:"wallet_address"`      // Destination/receiver wallet
	FromAddress   *string `json:"from_address,omitempty"` // Source/sender wallet

	// Transaction details
	Amount    int64  `json:"amount"`
	TokenType string `json:"token_type"`
	Memo      string `json:"memo,omitempty"`

	// Timing information
	Timestamp       time.Time `json:"timestamp"`
	BlockTime       time.Time `json:"block_time"`
	ConfirmationStatus string `json:"confirmation_status"`

	// Metadata
	PublishedAt time.Time `json:"published_at"`
}

// FromDBTransaction converts a database transaction to a TransactionEvent for publishing.
func FromDBTransaction(txn *db.Transaction) *TransactionEvent {
	event := &TransactionEvent{
		Signature:          txn.Signature,
		Slot:               txn.Slot,
		WalletAddress:      txn.WalletAddress,
		FromAddress:        txn.FromAddress,
		Amount:             txn.Amount,
		BlockTime:          txn.BlockTime,
		Timestamp:          txn.CreatedAt,
		ConfirmationStatus: txn.ConfirmationStatus,
		PublishedAt:        time.Now().UTC(),
	}

	// Convert optional string fields
	if txn.TokenMint != nil {
		event.TokenType = *txn.TokenMint
	}
	if txn.Memo != nil {
		event.Memo = *txn.Memo
	}

	return event
}
