package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// SSEPublisher manages Server-Sent Events connections for transaction streaming.
type SSEPublisher struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	logger *slog.Logger
}

// NewSSEPublisher creates a new SSE publisher that subscribes to NATS internally.
func NewSSEPublisher(natsURL string, logger *slog.Logger) (*SSEPublisher, error) {
	// Connect to NATS
	nc, err := nats.Connect(natsURL,
		nats.Name("forohtoo-sse-publisher"),
		nats.Timeout(10*time.Second),
		nats.ReconnectWait(1*time.Second),
		nats.MaxReconnects(-1),
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

	logger.Info("SSE publisher initialized", "nats_url", natsURL)

	return &SSEPublisher{
		nc:     nc,
		js:     js,
		logger: logger,
	}, nil
}

// Close closes the NATS connection.
func (p *SSEPublisher) Close() error {
	if p.nc != nil {
		p.nc.Close()
		p.logger.Info("SSE publisher closed")
	}
	return nil
}

// handleStreamTransactions handles SSE streaming for transactions.
// If address path parameter is empty, streams all wallets. Otherwise, streams specific wallet.
func handleStreamTransactions(publisher *SSEPublisher, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get wallet address from URL path parameter (may be empty for "all wallets" route)
		address := r.PathValue("address")

		// Determine subject filter and description for logging/responses
		var subject string
		var walletDesc string
		if address == "" {
			subject = "txns.*"
			walletDesc = "all wallets"
		} else {
			subject = fmt.Sprintf("txns.%s", address)
			walletDesc = address
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Flush headers immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		logger.DebugContext(r.Context(), "SSE client connected",
			"wallet", walletDesc,
			"remote_addr", r.RemoteAddr,
		)

		// Create ephemeral consumer for this connection
		cons, err := publisher.js.CreateOrUpdateConsumer(r.Context(), natspkg.StreamName, jetstream.ConsumerConfig{
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy, // Only deliver new messages after consumer creation
			// Ephemeral - will be deleted when connection closes
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create consumer",
				"wallet", walletDesc,
				"error", err,
			)
			fmt.Fprintf(w, "event: error\ndata: {\"error\": \"failed to subscribe\"}\n\n")
			return
		}

		// Create buffered channel for messages
		msgChan := make(chan jetstream.Msg, 10)
		doneChan := make(chan struct{})

		// Start consuming messages
		go func() {
			defer close(doneChan)
			cc, err := cons.Consume(func(msg jetstream.Msg) {
				select {
				case msgChan <- msg:
				case <-r.Context().Done():
					return
				}
			})
			if err != nil {
				logger.ErrorContext(r.Context(), "failed to start consuming messages",
					"error", err,
				)
				return
			}
			// Wait for context to be done, then stop consuming
			<-r.Context().Done()
			cc.Stop()
		}()

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"wallet\":\"%s\"}\n\n", walletDesc)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Create ticker for keepalive comments (every 10 seconds)
		keepalive := time.NewTicker(10 * time.Second)
		defer keepalive.Stop()

		// Stream events to client
		for {
			select {
			case <-keepalive.C:
				// Send keepalive comment to prevent timeout
				fmt.Fprintf(w, ": keepalive\n\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}

			case msg := <-msgChan:
				var event natspkg.TransactionEvent
				if err := json.Unmarshal(msg.Data(), &event); err != nil {
					logger.WarnContext(r.Context(), "failed to unmarshal event",
						"error", err,
					)
					msg.Ack()
					continue
				}

				// Send transaction event
				data, err := json.Marshal(event)
				if err != nil {
					logger.WarnContext(r.Context(), "failed to marshal event",
						"error", err,
					)
					msg.Ack()
					continue
				}

				fmt.Fprintf(w, "event: transaction\ndata: %s\n\n", string(data))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}

				msg.Ack()

				logger.DebugContext(r.Context(), "sent transaction event",
					"wallet", address,
					"signature", event.Signature,
				)

			case <-r.Context().Done():
				// Client disconnected
				logger.DebugContext(r.Context(), "SSE client disconnected",
					"wallet", walletDesc,
					"remote_addr", r.RemoteAddr,
				)
				return

			case <-doneChan:
				// Consumer closed
				return
			}
		}
	})
}
