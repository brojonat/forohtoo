package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Publisher defines the interface for publishing transaction events to NATS.
type Publisher interface {
	// PublishTransaction publishes a single transaction event to JetStream.
	// The event is published to the subject "txns.{wallet_address}".
	PublishTransaction(ctx context.Context, event *TransactionEvent) error

	// PublishTransactionBatch publishes multiple transaction events.
	// This is more efficient than calling PublishTransaction multiple times.
	PublishTransactionBatch(ctx context.Context, events []*TransactionEvent) error

	// Close closes the connection to NATS.
	Close() error
}

// JetStreamPublisher publishes transaction events to NATS JetStream.
type JetStreamPublisher struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	logger *slog.Logger
}

const (
	// StreamName is the name of the JetStream stream for transactions.
	StreamName = "TRANSACTIONS"

	// StreamSubjects is the subject pattern for the stream.
	StreamSubjects = "txns.*"

	// StreamRetention is how long messages are retained (30 days by default).
	StreamRetention = 30 * 24 * time.Hour
)

// NewPublisher creates a new JetStream publisher.
// It connects to NATS and ensures the stream exists.
func NewPublisher(natsURL string, logger *slog.Logger) (*JetStreamPublisher, error) {
	// Connect to NATS
	nc, err := nats.Connect(natsURL,
		nats.Name("forohtoo-publisher"),
		nats.Timeout(10*time.Second),
		nats.ReconnectWait(1*time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Create JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	publisher := &JetStreamPublisher{
		nc:     nc,
		js:     js,
		logger: logger,
	}

	// Ensure stream exists
	if err := publisher.ensureStream(); err != nil {
		nc.Close()
		return nil, fmt.Errorf("failed to ensure stream exists: %w", err)
	}

	logger.Info("NATS publisher initialized",
		"url", natsURL,
		"stream", StreamName,
	)

	return publisher, nil
}

// ensureStream creates the JetStream stream if it doesn't exist.
func (p *JetStreamPublisher) ensureStream() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try to get existing stream
	stream, err := p.js.Stream(ctx, StreamName)
	if err == nil {
		// Stream exists, log info
		info, err := stream.Info(ctx)
		if err == nil {
			p.logger.Debug("JetStream stream already exists",
				"stream", StreamName,
				"messages", info.State.Msgs,
			)
		}
		return nil
	}

	// Stream doesn't exist, create it
	p.logger.Info("creating JetStream stream", "stream", StreamName)

	streamConfig := jetstream.StreamConfig{
		Name:        StreamName,
		Description: "Transaction events from Solana wallets",
		Subjects:    []string{StreamSubjects},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      StreamRetention,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
	}

	_, err = p.js.CreateStream(ctx, streamConfig)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	p.logger.Info("JetStream stream created successfully", "stream", StreamName)
	return nil
}

// PublishTransaction publishes a single transaction event.
func (p *JetStreamPublisher) PublishTransaction(ctx context.Context, event *TransactionEvent) error {
	subject := fmt.Sprintf("txns.%s", event.WalletAddress)

	// Marshal event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction event: %w", err)
	}

	// Publish to JetStream
	_, err = p.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish transaction: %w", err)
	}

	p.logger.Debug("published transaction event",
		"subject", subject,
		"signature", event.Signature,
		"wallet", event.WalletAddress,
	)

	return nil
}

// PublishTransactionBatch publishes multiple transaction events efficiently.
func (p *JetStreamPublisher) PublishTransactionBatch(ctx context.Context, events []*TransactionEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Publish each event (JetStream handles batching internally)
	for _, event := range events {
		if err := p.PublishTransaction(ctx, event); err != nil {
			// Log error but continue with other events
			p.logger.Error("failed to publish transaction in batch",
				"signature", event.Signature,
				"wallet", event.WalletAddress,
				"error", err,
			)
			// Don't fail the entire batch on one error
			continue
		}
	}

	p.logger.Debug("published transaction batch",
		"count", len(events),
	)

	return nil
}

// Close closes the connection to NATS.
func (p *JetStreamPublisher) Close() error {
	if p.nc != nil {
		p.nc.Close()
		p.logger.Info("NATS publisher closed")
	}
	return nil
}
