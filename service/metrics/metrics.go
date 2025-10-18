package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus collectors for the application.
// Following the explicit dependency injection pattern, this struct
// is passed to all components that need to record metrics.
type Metrics struct {
	// Solana RPC Metrics
	solanaRPCCallsTotal        *prometheus.CounterVec
	solanaRPCCallDuration      *prometheus.HistogramVec
	solanaRPCRateLimitHits     *prometheus.CounterVec
	solanaRPCRetries           *prometheus.CounterVec
	solanaRPCSignaturesPerCall *prometheus.HistogramVec

	// Transaction Processing Metrics
	transactionsFetchedTotal       *prometheus.CounterVec
	transactionsParsedTotal        *prometheus.CounterVec
	transactionsWrittenTotal       *prometheus.CounterVec
	transactionsSkippedTotal       *prometheus.CounterVec
	transactionsDeduplicationRatio *prometheus.GaugeVec

	// Workflow Metrics
	pollWorkflowDuration        *prometheus.HistogramVec
	pollWorkflowExecutionsTotal *prometheus.CounterVec
	pollActivityDuration        *prometheus.HistogramVec

	// Database Metrics
	dbQueryDuration   *prometheus.HistogramVec
	dbOperationsTotal *prometheus.CounterVec

	// HTTP Metrics
	httpRequestDuration  *prometheus.HistogramVec
	httpRequestsTotal    *prometheus.CounterVec
	sseActiveConnections *prometheus.GaugeVec
	sseEventsSent        *prometheus.CounterVec

	// NATS Metrics
	natsMessagesPublished *prometheus.CounterVec
	natsPublishDuration   *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance and registers all collectors.
// If registry is nil, prometheus.DefaultRegisterer is used.
func NewMetrics(registry prometheus.Registerer) *Metrics {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	factory := promauto.With(registry)

	return &Metrics{
		// Solana RPC Metrics
		solanaRPCCallsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "solana_rpc_calls_total",
				Help: "Total number of Solana RPC calls by method and status",
			},
			[]string{"method", "status", "endpoint"},
		),
		solanaRPCCallDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "solana_rpc_call_duration_seconds",
				Help:    "Duration of Solana RPC calls in seconds",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
			},
			[]string{"method", "endpoint"},
		),
		solanaRPCRateLimitHits: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "solana_rpc_rate_limit_hits_total",
				Help: "Total number of Solana RPC rate limit hits (429 errors)",
			},
			[]string{"endpoint"},
		),
		solanaRPCRetries: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "solana_rpc_retries_total",
				Help: "Total number of Solana RPC retry attempts",
			},
			[]string{"method", "reason"},
		),
		solanaRPCSignaturesPerCall: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "solana_rpc_signatures_per_call",
				Help:    "Number of signatures fetched per GetSignaturesForAddress call",
				Buckets: []float64{1, 10, 50, 100, 250, 500, 1000},
			},
			[]string{"endpoint"},
		),

		// Transaction Processing Metrics
		transactionsFetchedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "transactions_fetched_total",
				Help: "Total number of transactions fetched from Solana",
			},
			[]string{"wallet_address", "source"},
		),
		transactionsParsedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "transactions_parsed_total",
				Help: "Total number of transactions parsed",
			},
			[]string{"wallet_address", "status"},
		),
		transactionsWrittenTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "transactions_written_total",
				Help: "Total number of transactions written to database",
			},
			[]string{"wallet_address"},
		),
		transactionsSkippedTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "transactions_skipped_total",
				Help: "Total number of transactions skipped",
			},
			[]string{"wallet_address", "reason"},
		),
		transactionsDeduplicationRatio: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "transactions_deduplication_ratio",
				Help: "Ratio of skipped transactions to total transactions (0.0-1.0)",
			},
			[]string{"wallet_address"},
		),

		// Workflow Metrics
		pollWorkflowDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "poll_workflow_duration_seconds",
				Help:    "Duration of poll workflow execution in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
			},
			[]string{"wallet_address", "status"},
		),
		pollWorkflowExecutionsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "poll_workflow_executions_total",
				Help: "Total number of poll workflow executions",
			},
			[]string{"wallet_address", "status"},
		),
		pollActivityDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "poll_activity_duration_seconds",
				Help:    "Duration of poll workflow activities in seconds",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
			},
			[]string{"activity", "wallet_address"},
		),

		// Database Metrics
		dbQueryDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "db_query_duration_seconds",
				Help:    "Duration of database queries in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
			},
			[]string{"operation", "table"},
		),
		dbOperationsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_operations_total",
				Help: "Total number of database operations",
			},
			[]string{"operation", "status"},
		),

		// HTTP Metrics
		httpRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "Duration of HTTP requests in seconds",
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
			},
			[]string{"handler", "method", "status"},
		),
		httpRequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"handler", "method", "status"},
		),
		sseActiveConnections: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "sse_active_connections",
				Help: "Number of active SSE connections",
			},
			[]string{"wallet_address"},
		),
		sseEventsSent: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "sse_events_sent_total",
				Help: "Total number of SSE events sent",
			},
			[]string{"wallet_address", "event_type"},
		),

		// NATS Metrics
		natsMessagesPublished: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "nats_messages_published_total",
				Help: "Total number of NATS messages published",
			},
			[]string{"subject", "status"},
		),
		natsPublishDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "nats_publish_duration_seconds",
				Help:    "Duration of NATS publish operations in seconds",
				Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
			},
			[]string{"subject"},
		),
	}
}

// Solana RPC metric helpers

// RecordRPCCall records a Solana RPC call with duration.
func (m *Metrics) RecordRPCCall(method, status, endpoint string, duration float64) {
	m.solanaRPCCallsTotal.WithLabelValues(method, status, endpoint).Inc()
	m.solanaRPCCallDuration.WithLabelValues(method, endpoint).Observe(duration)
}

// RecordRateLimitHit records a rate limit hit (429 error).
func (m *Metrics) RecordRateLimitHit(endpoint string) {
	m.solanaRPCRateLimitHits.WithLabelValues(endpoint).Inc()
}

// RecordRPCRetry records a retry attempt.
func (m *Metrics) RecordRPCRetry(method, reason string) {
	m.solanaRPCRetries.WithLabelValues(method, reason).Inc()
}

// RecordRPCSignaturesPerCall records the number of signatures fetched.
func (m *Metrics) RecordRPCSignaturesPerCall(endpoint string, count float64) {
	m.solanaRPCSignaturesPerCall.WithLabelValues(endpoint).Observe(count)
}

// Transaction processing metric helpers

// RecordTransactionsFetched records transactions fetched from Solana.
func (m *Metrics) RecordTransactionsFetched(walletAddress, source string, count int) {
	m.transactionsFetchedTotal.WithLabelValues(walletAddress, source).Add(float64(count))
}

// RecordTransactionParsed records a transaction parse attempt.
func (m *Metrics) RecordTransactionParsed(walletAddress, status string) {
	m.transactionsParsedTotal.WithLabelValues(walletAddress, status).Inc()
}

// RecordTransactionsWritten records transactions written to database.
func (m *Metrics) RecordTransactionsWritten(walletAddress string, count int) {
	m.transactionsWrittenTotal.WithLabelValues(walletAddress).Add(float64(count))
}

// RecordTransactionsSkipped records transactions skipped.
func (m *Metrics) RecordTransactionsSkipped(walletAddress, reason string, count int) {
	m.transactionsSkippedTotal.WithLabelValues(walletAddress, reason).Add(float64(count))
}

// RecordDeduplicationRatio records the deduplication efficiency ratio.
func (m *Metrics) RecordDeduplicationRatio(walletAddress string, ratio float64) {
	m.transactionsDeduplicationRatio.WithLabelValues(walletAddress).Set(ratio)
}

// Workflow metric helpers

// RecordWorkflowDuration records workflow execution duration.
func (m *Metrics) RecordWorkflowDuration(walletAddress, status string, duration float64) {
	m.pollWorkflowDuration.WithLabelValues(walletAddress, status).Observe(duration)
	m.pollWorkflowExecutionsTotal.WithLabelValues(walletAddress, status).Inc()
}

// RecordActivityDuration records activity execution duration.
func (m *Metrics) RecordActivityDuration(activity, walletAddress string, duration float64) {
	m.pollActivityDuration.WithLabelValues(activity, walletAddress).Observe(duration)
}

// Database metric helpers

// RecordDBQuery records a database query with duration.
func (m *Metrics) RecordDBQuery(operation, table string, duration float64, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	m.dbQueryDuration.WithLabelValues(operation, table).Observe(duration)
	m.dbOperationsTotal.WithLabelValues(operation, status).Inc()
}

// HTTP metric helpers

// RecordHTTPRequest records an HTTP request with duration.
func (m *Metrics) RecordHTTPRequest(handler, method string, statusCode int, duration float64) {
	status := statusCodeToString(statusCode)
	m.httpRequestDuration.WithLabelValues(handler, method, status).Observe(duration)
	m.httpRequestsTotal.WithLabelValues(handler, method, status).Inc()
}

// RecordSSEConnectionChange records a change in SSE connection count.
func (m *Metrics) RecordSSEConnectionChange(walletAddress string, delta float64) {
	m.sseActiveConnections.WithLabelValues(walletAddress).Add(delta)
}

// RecordSSEEventSent records an SSE event being sent.
func (m *Metrics) RecordSSEEventSent(walletAddress, eventType string) {
	m.sseEventsSent.WithLabelValues(walletAddress, eventType).Inc()
}

// NATS metric helpers

// RecordNATSPublish records a NATS publish operation.
func (m *Metrics) RecordNATSPublish(subject, status string, duration float64) {
	m.natsMessagesPublished.WithLabelValues(subject, status).Inc()
	m.natsPublishDuration.WithLabelValues(subject).Observe(duration)
}

// Helper functions

func statusCodeToString(code int) string {
	// Group status codes by class
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "unknown"
	}
}
