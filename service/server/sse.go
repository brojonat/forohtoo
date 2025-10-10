package server

import (
	"context"
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

// AwaitFilter defines criteria for filtering transactions.
type AwaitFilter struct {
	WorkflowID *string
	Signature  *string
}

// Await blocks until a transaction matching the filter arrives, or context times out.
func (p *SSEPublisher) Await(ctx context.Context, walletAddress string, filter AwaitFilter) (*natspkg.TransactionEvent, error) {
	subject := fmt.Sprintf("txns.%s", walletAddress)

	// Create ephemeral consumer
	cons, err := p.js.CreateOrUpdateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	// Create channel for messages
	msgChan := make(chan jetstream.Msg, 10)
	doneChan := make(chan struct{})

	// Start consuming messages
	go func() {
		defer close(doneChan)
		_, _ = cons.Consume(func(msg jetstream.Msg) {
			select {
			case msgChan <- msg:
			case <-ctx.Done():
				return
			}
		})
	}()

	// Wait for matching transaction
	for {
		select {
		case msg := <-msgChan:
			var event natspkg.TransactionEvent
			if err := json.Unmarshal(msg.Data(), &event); err != nil {
				p.logger.Warn("failed to unmarshal event", "error", err)
				msg.Ack()
				continue
			}

			// Check if event matches filter
			if matchesFilter(&event, filter) {
				msg.Ack()
				return &event, nil
			}

			msg.Ack()

		case <-ctx.Done():
			return nil, ctx.Err()

		case <-doneChan:
			return nil, fmt.Errorf("consumer closed unexpectedly")
		}
	}
}

// matchesFilter checks if a transaction event matches the filter criteria.
func matchesFilter(event *natspkg.TransactionEvent, filter AwaitFilter) bool {
	// Check signature match
	if filter.Signature != nil && *filter.Signature != event.Signature {
		return false
	}

	// Check workflow_id in memo (if specified)
	if filter.WorkflowID != nil {
		if event.Memo == "" {
			return false
		}

		// Try to parse memo as JSON
		var memoData struct {
			WorkflowID string `json:"workflow_id"`
		}
		if err := json.Unmarshal([]byte(event.Memo), &memoData); err != nil {
			return false
		}

		if memoData.WorkflowID != *filter.WorkflowID {
			return false
		}
	}

	return true
}

// handleStreamTransactions handles SSE streaming for a specific wallet.
func handleStreamTransactions(publisher *SSEPublisher, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get wallet address from URL path parameter
		address := r.PathValue("address")
		if address == "" {
			writeError(w, "wallet address is required", http.StatusBadRequest)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*") // Configure CORS as needed

		// Flush headers immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		logger.DebugContext(r.Context(), "SSE client connected",
			"wallet", address,
			"remote_addr", r.RemoteAddr,
		)

		subject := fmt.Sprintf("txns.%s", address)

		// Create ephemeral consumer for this connection
		cons, err := publisher.js.CreateOrUpdateConsumer(r.Context(), natspkg.StreamName, jetstream.ConsumerConfig{
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			// Ephemeral - will be deleted when connection closes
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create consumer",
				"wallet", address,
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
			_, _ = cons.Consume(func(msg jetstream.Msg) {
				select {
				case msgChan <- msg:
				case <-r.Context().Done():
					return
				}
			})
		}()

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"wallet\":\"%s\"}\n\n", address)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Stream events to client
		for {
			select {
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
					"wallet", address,
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

// handleStreamAllTransactions handles SSE streaming for all wallets.
func handleStreamAllTransactions(publisher *SSEPublisher, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*") // Configure CORS as needed

		// Flush headers immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		logger.DebugContext(r.Context(), "SSE client connected (all wallets)",
			"remote_addr", r.RemoteAddr,
		)

		// Subscribe to all transaction subjects
		subject := "txns.*"

		// Create ephemeral consumer for this connection
		cons, err := publisher.js.CreateOrUpdateConsumer(r.Context(), natspkg.StreamName, jetstream.ConsumerConfig{
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create consumer",
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
			_, _ = cons.Consume(func(msg jetstream.Msg) {
				select {
				case msgChan <- msg:
				case <-r.Context().Done():
					return
				}
			})
		}()

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"message\":\"subscribed to all wallets\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Stream events to client
		for {
			select {
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
					"wallet", event.WalletAddress,
					"signature", event.Signature,
				)

			case <-r.Context().Done():
				// Client disconnected
				logger.DebugContext(r.Context(), "SSE client disconnected (all wallets)",
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

// handleAwaitTransaction handles blocking HTTP requests that wait for a specific transaction.
func handleAwaitTransaction(publisher *SSEPublisher, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get wallet address from URL path
		address := r.PathValue("address")
		if address == "" {
			writeError(w, "wallet address is required", http.StatusBadRequest)
			return
		}

		// Parse query parameters for filtering
		query := r.URL.Query()
		workflowID := query.Get("workflow_id")
		signature := query.Get("signature")

		if workflowID == "" && signature == "" {
			writeError(w, "must specify workflow_id or signature filter", http.StatusBadRequest)
			return
		}

		// Build filter
		filter := AwaitFilter{}
		if workflowID != "" {
			filter.WorkflowID = &workflowID
		}
		if signature != "" {
			filter.Signature = &signature
		}

		// Parse optional timeout (default 5 minutes, max 10 minutes)
		timeout := 5 * time.Minute
		if timeoutStr := query.Get("timeout"); timeoutStr != "" {
			if d, err := time.ParseDuration(timeoutStr); err == nil {
				if d > 10*time.Minute {
					d = 10 * time.Minute
				}
				timeout = d
			}
		}

		// Create context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		logger.InfoContext(ctx, "awaiting transaction",
			"wallet", address,
			"workflow_id", workflowID,
			"signature", signature,
			"timeout", timeout,
		)

		// Block until transaction arrives or timeout
		event, err := publisher.Await(ctx, address, filter)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				writeError(w, "timeout waiting for transaction", http.StatusRequestTimeout)
				return
			}
			logger.ErrorContext(ctx, "error awaiting transaction",
				"error", err,
				"wallet", address,
			)
			writeError(w, fmt.Sprintf("failed to await transaction: %v", err), http.StatusInternalServerError)
			return
		}

		logger.InfoContext(ctx, "transaction received",
			"wallet", address,
			"signature", event.Signature,
		)

		// Return transaction as JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(event)
	})
}

