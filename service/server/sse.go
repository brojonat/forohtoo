package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// SSEPublisher manages Server-Sent Events connections for transaction streaming.
type SSEPublisher struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	logger *slog.Logger
	store  *db.Store
}

// NewSSEPublisher creates a new SSE publisher that subscribes to NATS internally.
func NewSSEPublisher(natsURL string, store *db.Store, logger *slog.Logger) (*SSEPublisher, error) {
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
		store:  store,
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
		// Disable write deadline for SSE streaming (long-lived connection)
		// The default server WriteTimeout of 15s would kill the connection
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			logger.WarnContext(r.Context(), "failed to disable write deadline", "error", err)
		}

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

		// Send initial connection event
		fmt.Fprintf(w, "event: connected\ndata: {\"wallet\":\"%s\"}\n\n", walletDesc)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// 1) Parse and validate lookback parameter
		lookbackParam := r.URL.Query().Get("lookback")
		var lookback time.Duration
		var err error

		if lookbackParam != "" {
			lookback, err = time.ParseDuration(lookbackParam)
			if err != nil {
				logger.WarnContext(r.Context(), "invalid lookback parameter", "lookback", lookbackParam, "error", err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid lookback duration format"})
				return
			}

			if lookback < 0 {
				logger.WarnContext(r.Context(), "negative lookback parameter", "lookback", lookback)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "lookback must be non-negative"})
				return
			}
		}

		// 2) Send historical transactions if lookback > 0
		if lookback > 0 {
			start := time.Now().Add(-lookback)
			end := time.Now()

			// Fetch historical transactions from database
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			historical, err := publisher.store.ListTransactionsByTimeRange(ctx, start, end)
			if err != nil {
				logger.ErrorContext(r.Context(), "failed to load historical transactions", "error", err)
				fmt.Fprintf(w, "event: error\ndata: {\"error\": \"failed to load history\"}\n\n")
				return
			}

			// Filter by wallet if requested
			if address != "" {
				filtered := make([]*db.Transaction, 0, len(historical))
				for _, t := range historical {
					if t.WalletAddress == address {
						filtered = append(filtered, t)
					}
				}
				historical = filtered
			}

		// Limit to 1000 events maximum
		const maxHistoricalEvents = 1000
		if len(historical) > maxHistoricalEvents {
			historical = historical[:maxHistoricalEvents]
		}

		// Send each historical transaction as individual transaction events
		for _, t := range historical {
			event := natspkg.FromDBTransaction(t)
			payload, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: transaction\ndata: %s\n\n", string(payload))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		}

		// 3) Switch to live streaming via NATS
		cons, err := publisher.js.CreateOrUpdateConsumer(r.Context(), natspkg.StreamName, jetstream.ConsumerConfig{
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy,
		})
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to create consumer",
				"wallet", walletDesc,
				"error", err,
			)
			fmt.Fprintf(w, "event: error\ndata: {\"error\": \"failed to subscribe\"}\n\n")
			return
		}

		msgChan := make(chan jetstream.Msg, 64)
		doneChan := make(chan struct{})

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
				logger.ErrorContext(r.Context(), "failed to start consuming messages", "error", err)
				return
			}
			<-r.Context().Done()
			cc.Stop()
		}()

		keepalive := time.NewTicker(10 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-keepalive.C:
				fmt.Fprintf(w, ": keepalive\n\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			case msg := <-msgChan:
				var event natspkg.TransactionEvent
				if err := json.Unmarshal(msg.Data(), &event); err != nil {
					logger.WarnContext(r.Context(), "failed to unmarshal event", "error", err)
					msg.Ack()
					continue
				}
				data, _ := json.Marshal(event)
				fmt.Fprintf(w, "event: transaction\ndata: %s\n\n", string(data))
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				msg.Ack()
			case <-r.Context().Done():
				logger.DebugContext(r.Context(), "SSE client disconnected", "wallet", walletDesc, "remote_addr", r.RemoteAddr)
				return
			case <-doneChan:
				return
			}
		}
	})
}
