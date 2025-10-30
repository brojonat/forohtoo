package server

import (
	"context"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/temporal"
)

// PaymentClientAdapter adapts the client.Client to the temporal.PaymentClientInterface.
// This bridges the gap between the client package's Transaction type and the temporal package's Transaction type.
type PaymentClientAdapter struct {
	client *client.Client
}

// NewPaymentClientAdapter creates a new payment client adapter.
func NewPaymentClientAdapter(c *client.Client) *PaymentClientAdapter {
	return &PaymentClientAdapter{client: c}
}

// Await awaits a transaction matching the provided matcher function.
// It converts between the client.Transaction and temporal.Transaction types.
func (p *PaymentClientAdapter) Await(ctx context.Context, address string, network string, lookback time.Duration, matcher func(*temporal.Transaction) bool) (*temporal.Transaction, error) {
	// Create an adapter function that converts client.Transaction to temporal.Transaction
	clientMatcher := func(t *client.Transaction) bool {
		// Convert client.Transaction to temporal.Transaction
		temporalTxn := &temporal.Transaction{
			Signature:          t.Signature,
			Slot:               t.Slot,
			WalletAddress:      t.WalletAddress,
			FromAddress:        t.FromAddress,
			Amount:             t.Amount,
			TokenType:          t.TokenType,
			Memo:               t.Memo,
			Timestamp:          t.Timestamp,
			BlockTime:          t.BlockTime,
			ConfirmationStatus: t.ConfirmationStatus,
			PublishedAt:        t.PublishedAt,
		}

		// Call the matcher with the converted transaction
		return matcher(temporalTxn)
	}

	// Call the underlying client's Await method
	txn, err := p.client.Await(ctx, address, network, lookback, clientMatcher)
	if err != nil {
		return nil, err
	}

	// Convert the result back to temporal.Transaction
	return &temporal.Transaction{
		Signature:          txn.Signature,
		Slot:               txn.Slot,
		WalletAddress:      txn.WalletAddress,
		FromAddress:        txn.FromAddress,
		Amount:             txn.Amount,
		TokenType:          txn.TokenType,
		Memo:               txn.Memo, // Already a pointer in both types
		Timestamp:          txn.Timestamp,
		BlockTime:          txn.BlockTime,
		ConfirmationStatus: txn.ConfirmationStatus,
		PublishedAt:        txn.PublishedAt,
	}, nil
}
