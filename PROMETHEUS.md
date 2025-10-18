# Prometheus Metrics Implementation Plan

## Overview

This document outlines the plan to add comprehensive Prometheus monitoring to the forohtoo service, with special emphasis on tracking Solana RPC endpoint usage to prevent rate limiting and optimize costs.

## Current State Analysis

### Deduplication Strategy (Already Implemented ✅)

The system **already prevents redundant RPC calls** through a multi-layer deduplication strategy:

1. **Database Query** (`temporal/workflow.go:74-81`):
   - Queries DB for transaction signatures from last 24 hours
   - Passes existing signatures to `PollSolana` activity

2. **Client-Level Skip** (`solana/client.go:95-101`):
   - Creates lookup map of existing signatures
   - **Skips expensive `GetTransaction` calls** for known signatures
   - Only fetches signature metadata once (cheap operation)

3. **Database Constraint** (`temporal/activities.go:277-283`):
   - Gracefully handles duplicate key constraint violations
   - Tracks skipped count

**Conclusion**: No redundant RPC calls are being made. The system is efficient.

### What's Missing: Observability

While the system is well-designed, we have **zero visibility** into:
- RPC call volume and latency per endpoint
- Rate limit hits and retry behavior
- Transaction deduplication efficiency metrics
- Database query performance
- Overall system throughput

## Metrics Architecture

Following the project's explicit dependency injection pattern (see CLAUDE.md), we will:

1. Create a `service/metrics` package with Prometheus collectors
2. Inject metrics as explicit dependencies to all components
3. Instrument at critical integration points
4. Expose metrics via `/metrics` HTTP endpoint

### Design Principles

- **Explicit Dependencies**: Pass `Metrics` struct to components that need it
- **No Global State**: No package-level Prometheus registries
- **Composable**: Metrics middleware that wraps existing handlers
- **Minimal Overhead**: Use prometheus best practices (counters, histograms, gauges)

## Metrics Specification

### 1. Solana RPC Metrics (Priority: CRITICAL)

**Purpose**: Track RPC usage to prevent rate limiting and optimize costs

```go
// Counter: Total RPC calls by method and status
solana_rpc_calls_total{method="GetSignaturesForAddress|GetTransaction", status="success|error|rate_limited", endpoint="mainnet|devnet|custom"}

// Histogram: RPC call duration in seconds
solana_rpc_call_duration_seconds{method="GetSignaturesForAddress|GetTransaction", endpoint="mainnet|devnet|custom"}
// Buckets: [0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0]

// Counter: Rate limit hits (429 errors)
solana_rpc_rate_limit_hits_total{endpoint="mainnet|devnet|custom"}

// Counter: Retry attempts
solana_rpc_retries_total{method="GetSignaturesForAddress|GetTransaction", reason="rate_limit|timeout|parse_error"}

// Histogram: Signatures fetched per call
solana_rpc_signatures_per_call{endpoint="mainnet|devnet|custom"}
// Buckets: [1, 10, 50, 100, 250, 500, 1000]
```

**Instrumentation Location**: `service/solana/client.go`
- Wrap `GetSignaturesForAddress` (line 71)
- Wrap `GetTransaction` (line 116)
- Track retry loops (lines 110-154)
- Track rate limit detection (line 122)

**Dependencies Needed**: Pass `*metrics.Metrics` to `solana.NewClient()`

---

### 2. Transaction Processing Metrics

**Purpose**: Track transaction parsing, deduplication efficiency, and throughput

```go
// Counter: Transactions fetched from Solana
transactions_fetched_total{wallet_address="...", source="main_wallet|usdc_ata"}

// Counter: Transactions parsed successfully vs errors
transactions_parsed_total{wallet_address="...", status="success|error|fallback_metadata_only"}

// Counter: Transactions written to database
transactions_written_total{wallet_address="..."}

// Counter: Transactions skipped (already exist or parse errors)
transactions_skipped_total{wallet_address="...", reason="already_exists|parse_error|already_fetched"}

// Gauge: Deduplication efficiency (0.0-1.0, calculated as skipped/fetched)
transactions_deduplication_ratio{wallet_address="..."}
```

**Instrumentation Locations**:
- `service/solana/client.go:93-101` - Track skipped transactions (already_fetched)
- `service/solana/client.go:169-179` - Track parse failures
- `service/temporal/activities.go:240-295` - Track writes and skips

**Dependencies Needed**: Pass `*metrics.Metrics` to `temporal.NewActivities()`

---

### 3. Temporal Workflow Metrics

**Purpose**: Track workflow execution times and success rates

```go
// Histogram: Workflow duration in seconds
poll_workflow_duration_seconds{wallet_address="...", status="success|error"}
// Buckets: [1, 5, 10, 30, 60, 120, 300]

// Counter: Workflow executions
poll_workflow_executions_total{wallet_address="...", status="success|error"}

// Histogram: Activity duration in seconds
poll_activity_duration_seconds{activity="GetExistingSignatures|PollSolana|WriteTransactions", wallet_address="..."}
// Buckets: [0.1, 0.5, 1, 5, 10, 30, 60]
```

**Instrumentation Locations**:
- `service/temporal/workflow.go:46-240` - Wrap workflow execution
- `service/temporal/activities.go` - Wrap each activity function

**Dependencies Needed**: Pass `*metrics.Metrics` to `temporal.NewActivities()`

---

### 4. Database Metrics

**Purpose**: Track database query performance

```go
// Histogram: Database query duration in seconds
db_query_duration_seconds{operation="CreateTransaction|GetExistingSignatures|UpdateWalletPollTime", table="transactions|wallets"}
// Buckets: [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0]

// Counter: Database operations
db_operations_total{operation="...", status="success|error"}
```

**Instrumentation Locations**:
- `service/db/store.go` - Wrap key operations (CreateTransaction, GetTransactionSignaturesByWallet, UpdateWalletPollTime)

**Dependencies Needed**: Pass `*metrics.Metrics` to `db.NewStore()`

---

### 5. HTTP Handler Metrics (Standard)

**Purpose**: Track HTTP request performance and error rates

```go
// Histogram: HTTP request duration in seconds
http_request_duration_seconds{handler="/api/v1/wallets|/api/v1/stream/...", method="GET|POST|DELETE", status="200|400|404|500"}
// Buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5]

// Counter: HTTP requests total
http_requests_total{handler="...", method="...", status="..."}

// Gauge: Active SSE connections
sse_active_connections{wallet_address="all|specific"}

// Counter: SSE events sent
sse_events_sent_total{wallet_address="...", event_type="connected|transaction|error"}
```

**Instrumentation Locations**:
- `service/server/handlers.go` - Add metrics middleware to all handlers
- `service/server/sse.go` - Track SSE connections and events

**Dependencies Needed**: Pass `*metrics.Metrics` to handler factory functions

---

### 6. NATS Publishing Metrics

**Purpose**: Track message publishing performance

```go
// Counter: NATS messages published
nats_messages_published_total{subject="txns.*", status="success|error"}

// Histogram: NATS publish duration in seconds
nats_publish_duration_seconds{subject="txns.*"}
// Buckets: [0.001, 0.005, 0.01, 0.05, 0.1, 0.5]
```

**Instrumentation Locations**:
- `service/nats/publisher.go` - Wrap `PublishTransaction` and `PublishTransactionBatch`

**Dependencies Needed**: Pass `*metrics.Metrics` to `nats.NewPublisher()`

---

## Implementation Plan

### Phase 1: Core Metrics Infrastructure

**Files to Create**:

1. **`service/metrics/metrics.go`**
   - Define `Metrics` struct holding all prometheus collectors
   - Constructor: `NewMetrics() *Metrics`
   - Register all metrics with prometheus default registry
   - Export methods for incrementing/observing each metric

2. **`service/metrics/middleware.go`**
   - HTTP middleware: `HTTPMetricsMiddleware(metrics *Metrics, handler string) func(http.Handler) http.Handler`
   - Wraps handlers to track request duration and status codes

3. **`service/metrics/instrumentation.go`**
   - Helper functions for timing operations
   - `func Timer(histogram prometheus.Histogram) func()` - Returns a function to call when done
   - Example: `defer metrics.Timer(m.dbQueryDuration.WithLabelValues("CreateTransaction"))("transactions")()`

**Example Structure** (`service/metrics/metrics.go`):

```go
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
    solanaRPCCallsTotal          *prometheus.CounterVec
    solanaRPCCallDuration        *prometheus.HistogramVec
    solanaRPCRateLimitHits       *prometheus.CounterVec
    solanaRPCRetries             *prometheus.CounterVec
    solanaRPCSignaturesPerCall   *prometheus.HistogramVec

    // Transaction Processing Metrics
    transactionsFetchedTotal     *prometheus.CounterVec
    transactionsParsedTotal      *prometheus.CounterVec
    transactionsWrittenTotal     *prometheus.CounterVec
    transactionsSkippedTotal     *prometheus.CounterVec
    transactionsDeduplicationRatio *prometheus.GaugeVec

    // Workflow Metrics
    pollWorkflowDuration         *prometheus.HistogramVec
    pollWorkflowExecutionsTotal  *prometheus.CounterVec
    pollActivityDuration         *prometheus.HistogramVec

    // Database Metrics
    dbQueryDuration              *prometheus.HistogramVec
    dbOperationsTotal            *prometheus.CounterVec

    // HTTP Metrics
    httpRequestDuration          *prometheus.HistogramVec
    httpRequestsTotal            *prometheus.CounterVec
    sseActiveConnections         *prometheus.GaugeVec
    sseEventsSent                *prometheus.CounterVec

    // NATS Metrics
    natsMessagesPublished        *prometheus.CounterVec
    natsPublishDuration          *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance and registers all collectors.
func NewMetrics(registry prometheus.Registerer) *Metrics {
    if registry == nil {
        registry = prometheus.DefaultRegisterer
    }

    factory := promauto.With(registry)

    return &Metrics{
        solanaRPCCallsTotal: factory.NewCounterVec(
            prometheus.CounterOpts{
                Name: "solana_rpc_calls_total",
                Help: "Total number of Solana RPC calls by method and status",
            },
            []string{"method", "status", "endpoint"},
        ),
        // ... define all other metrics ...
    }
}

// Solana RPC metric helpers

func (m *Metrics) RecordRPCCall(method, status, endpoint string, duration float64) {
    m.solanaRPCCallsTotal.WithLabelValues(method, status, endpoint).Inc()
    m.solanaRPCCallDuration.WithLabelValues(method, endpoint).Observe(duration)
}

func (m *Metrics) RecordRateLimitHit(endpoint string) {
    m.solanaRPCRateLimitHits.WithLabelValues(endpoint).Inc()
}

func (m *Metrics) RecordRPCRetry(method, reason string) {
    m.solanaRPCRetries.WithLabelValues(method, reason).Inc()
}

// ... more helper methods for other metrics ...
```

---

### Phase 2: Instrument Solana RPC Client

**File**: `service/solana/client.go`

**Changes**:

1. Add `metrics *metrics.Metrics` field to `Client` struct
2. Update `NewClient()` to accept metrics parameter
3. Instrument key operations:

```go
// Before (line 71)
signatures, err := c.rpc.GetSignaturesForAddress(ctx, params.Wallet, opts)

// After
start := time.Now()
signatures, err := c.rpc.GetSignaturesForAddress(ctx, params.Wallet, opts)
status := "success"
if err != nil {
    status = "error"
}
if c.metrics != nil {
    c.metrics.RecordRPCCall(
        "GetSignaturesForAddress",
        status,
        c.endpoint, // Add endpoint field to Client
        time.Since(start).Seconds(),
    )
    c.metrics.RecordRPCSignaturesPerCall(c.endpoint, float64(len(signatures)))
}

// Track skipped transactions (line 95-101)
if _, exists := existingSigs[sig.Signature.String()]; exists {
    if c.metrics != nil {
        c.metrics.RecordTransactionSkipped(params.Wallet.String(), "already_fetched")
    }
    c.logger.DebugContext(ctx, "skipping already processed transaction", ...)
    continue
}

// Track rate limit hits (line 122)
if strings.Contains(err.Error(), "429") {
    if c.metrics != nil {
        c.metrics.RecordRateLimitHit(c.endpoint)
        c.metrics.RecordRPCRetry("GetTransaction", "rate_limit")
    }
    // ... existing retry logic
}

// Track transaction parsing (line 169-179)
txn, err := parseTransactionFromResult(sig, result)
if err != nil {
    if c.metrics != nil {
        c.metrics.RecordTransactionParsed(params.Wallet.String(), "error")
    }
    // ... fallback to metadata
} else {
    if c.metrics != nil {
        c.metrics.RecordTransactionParsed(params.Wallet.String(), "success")
    }
}
```

**Dependency Injection Update**:

Update all `solana.NewClient()` call sites to pass metrics:
- `cmd/server/main.go`
- `cmd/worker/main.go`

---

### Phase 3: Instrument Temporal Activities

**File**: `service/temporal/activities.go`

**Changes**:

1. Add `metrics *metrics.Metrics` field to `Activities` struct
2. Update `NewActivities()` to accept metrics parameter
3. Instrument activities:

```go
// PollSolana activity (line 121)
func (a *Activities) PollSolana(ctx context.Context, input PollSolanaInput) (*PollSolanaResult, error) {
    start := time.Now()
    defer func() {
        if a.metrics != nil {
            a.metrics.RecordActivityDuration("PollSolana", input.Address, time.Since(start).Seconds())
        }
    }()

    // ... existing logic ...

    if a.metrics != nil {
        a.metrics.RecordTransactionsFetched(input.Address, "main_wallet", len(transactions))
    }

    return result, nil
}

// WriteTransactions activity (line 230)
func (a *Activities) WriteTransactions(ctx context.Context, input WriteTransactionsInput) (*WriteTransactionsResult, error) {
    start := time.Now()
    defer func() {
        if a.metrics != nil {
            a.metrics.RecordActivityDuration("WriteTransactions", input.WalletAddress, time.Since(start).Seconds())
        }
    }()

    // ... existing write logic ...

    if a.metrics != nil {
        a.metrics.RecordTransactionsWritten(input.WalletAddress, written)
        a.metrics.RecordTransactionsSkipped(input.WalletAddress, "already_exists", skipped)

        // Calculate deduplication ratio
        total := float64(len(input.Transactions))
        if total > 0 {
            ratio := float64(skipped) / total
            a.metrics.RecordDeduplicationRatio(input.WalletAddress, ratio)
        }
    }

    return &WriteTransactionsResult{Written: written, Skipped: skipped}, nil
}
```

**Dependency Injection Update**:

Update `temporal.NewActivities()` call sites to pass metrics:
- `cmd/worker/main.go`

---

### Phase 4: Instrument HTTP Handlers

**File**: `service/server/handlers.go`

**Changes**:

1. Create metrics middleware wrapper
2. Apply to all handlers in `server.go`

**Middleware** (`service/server/middleware.go` - new file):

```go
package server

import (
    "net/http"
    "time"
    "github.com/brojonat/forohtoo/service/metrics"
)

// metricsMiddleware wraps a handler to record HTTP metrics.
func metricsMiddleware(m *metrics.Metrics, handlerName string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()

            // Wrap ResponseWriter to capture status code
            wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

            next.ServeHTTP(wrapped, r)

            duration := time.Since(start).Seconds()
            status := wrapped.statusCode

            if m != nil {
                m.RecordHTTPRequest(handlerName, r.Method, status, duration)
            }
        })
    }
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (w *responseWriter) WriteHeader(statusCode int) {
    w.statusCode = statusCode
    w.ResponseWriter.WriteHeader(statusCode)
}
```

**Apply Middleware** (`service/server/server.go`):

```go
// Add metrics field to Server struct
type Server struct {
    // ... existing fields ...
    metrics *metrics.Metrics
}

// Update NewServer to accept metrics
func NewServer(addr string, store *db.Store, scheduler temporal.Scheduler, ssePublisher *SSEPublisher, renderer *TemplateRenderer, metrics *metrics.Metrics, logger *slog.Logger) *Server {
    // ... existing setup ...
    s := &Server{
        // ... existing fields ...
        metrics: metrics,
    }
    return s
}

// Update route setup (line 90-110)
mux.Handle("/api/v1/wallets",
    corsMiddleware(
        metricsMiddleware(s.metrics, "/api/v1/wallets")(
            handleRegisterWalletWithScheduler(s.store, s.scheduler, s.metrics, s.logger),
        ),
    ),
)
```

**Update Handler Factories**:

Pass metrics to handler functions so they can record domain-specific metrics:

```go
func handleRegisterWalletWithScheduler(
    store *db.Store,
    scheduler temporal.Scheduler,
    metrics *metrics.Metrics, // NEW
    logger *slog.Logger,
) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ... existing logic ...

        // Example: Record wallet registration
        if metrics != nil {
            metrics.RecordWalletRegistered(wallet.Address)
        }
    })
}
```

---

### Phase 5: Add Metrics HTTP Endpoint

**File**: `service/server/server.go`

**Changes**:

Add `/metrics` endpoint to expose Prometheus metrics:

```go
import (
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

// In setupRoutes() (around line 90)
mux.Handle("/metrics", promhttp.Handler())
```

This exposes metrics at `http://localhost:8080/metrics` in Prometheus text format.

---

### Phase 6: Instrument Database Operations (Optional)

**File**: `service/db/store.go`

**Changes**:

1. Add `metrics *metrics.Metrics` field to `Store` struct
2. Update `NewStore()` to accept metrics parameter
3. Wrap key operations:

```go
func (s *Store) CreateTransaction(ctx context.Context, params CreateTransactionParams) (*Transaction, error) {
    start := time.Now()
    defer func() {
        if s.metrics != nil {
            s.metrics.RecordDBQuery("CreateTransaction", "transactions", time.Since(start).Seconds())
        }
    }()

    // ... existing logic ...
}

func (s *Store) GetTransactionSignaturesByWallet(ctx context.Context, walletAddress string, since *time.Time) ([]string, error) {
    start := time.Now()
    defer func() {
        if s.metrics != nil {
            s.metrics.RecordDBQuery("GetTransactionSignaturesByWallet", "transactions", time.Since(start).Seconds())
        }
    }()

    // ... existing logic ...
}
```

---

### Phase 7: Instrument NATS Publisher (Optional)

**File**: `service/nats/publisher.go`

**Changes**:

1. Add `metrics *metrics.Metrics` field to `Publisher` struct
2. Update `NewPublisher()` to accept metrics parameter
3. Wrap publish operations:

```go
func (p *Publisher) PublishTransaction(ctx context.Context, event *TransactionEvent) error {
    start := time.Now()

    // ... existing publish logic ...

    status := "success"
    if err != nil {
        status = "error"
    }

    if p.metrics != nil {
        p.metrics.RecordNATSPublish(subject, status, time.Since(start).Seconds())
    }

    return err
}
```

---

## Deployment Considerations

### Environment Variables

No new environment variables required. Metrics are always enabled.

### Kubernetes Setup

**Prometheus ServiceMonitor** (for Prometheus Operator):

Create `k8s/prod/servicemonitor.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: forohtoo-server
  namespace: default
spec:
  selector:
    matchLabels:
      app: forohtoo-server
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

**Service Annotations** (for Prometheus scraping):

Update `k8s/prod/server-deployment.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: forohtoo-server
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: "/metrics"
spec:
  # ... existing spec ...
```

### Grafana Dashboard

**Key Panels to Create**:

1. **RPC Health Dashboard**:
   - RPC calls per second (rate by method)
   - RPC latency percentiles (p50, p95, p99)
   - Rate limit hits over time
   - Retry count by reason

2. **Transaction Processing Dashboard**:
   - Transactions fetched vs written vs skipped
   - Deduplication efficiency gauge
   - Parse success rate
   - Workflow execution time

3. **System Health Dashboard**:
   - HTTP request rate and latency
   - Database query latency
   - Active SSE connections
   - NATS publish rate

**Example PromQL Queries**:

```promql
# RPC calls per second by method
rate(solana_rpc_calls_total[5m])

# RPC latency p95
histogram_quantile(0.95, rate(solana_rpc_call_duration_seconds_bucket[5m]))

# Rate limit hit rate
rate(solana_rpc_rate_limit_hits_total[5m])

# Deduplication efficiency (should be high for active wallets)
transactions_deduplication_ratio

# Transactions written per minute
rate(transactions_written_total[1m])

# HTTP request error rate
rate(http_requests_total{status=~"5.."}[5m]) / rate(http_requests_total[5m])
```

---

## Testing Strategy

### Unit Tests

Create `service/metrics/metrics_test.go`:

```go
func TestMetrics_RecordRPCCall(t *testing.T) {
    registry := prometheus.NewRegistry()
    m := NewMetrics(registry)

    m.RecordRPCCall("GetSignaturesForAddress", "success", "mainnet", 0.123)

    // Verify metric was recorded
    metrics, err := registry.Gather()
    require.NoError(t, err)

    // Assert counter incremented
    // Assert histogram observed value
}
```

### Integration Tests

Add to existing integration tests:

```go
func TestPollWorkflow_RecordsMetrics(t *testing.T) {
    // Setup: Create test registry and metrics
    registry := prometheus.NewRegistry()
    m := metrics.NewMetrics(registry)

    // Run workflow with instrumented activities
    // ...

    // Assert: Verify expected metrics were recorded
    metricFamilies, _ := registry.Gather()
    assertMetricExists(t, metricFamilies, "poll_workflow_duration_seconds")
    assertMetricExists(t, metricFamilies, "transactions_fetched_total")
}
```

### Manual Testing

```bash
# Start server with metrics
make run-server

# Trigger some activity (register wallet, poll)
forohtoo wallet add YOUR_WALLET

# Check metrics endpoint
curl http://localhost:8080/metrics | grep solana_rpc

# Expected output:
# solana_rpc_calls_total{method="GetSignaturesForAddress",status="success",endpoint="mainnet"} 1
# solana_rpc_call_duration_seconds_bucket{method="GetSignaturesForAddress",endpoint="mainnet",le="0.5"} 1
# ...
```

---

## Alerting Rules (Prometheus)

**Create `prometheus/alerts.yml`**:

```yaml
groups:
  - name: solana_rpc
    interval: 30s
    rules:
      # Alert on high rate limit hit rate
      - alert: SolanaRPCRateLimitHigh
        expr: rate(solana_rpc_rate_limit_hits_total[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High Solana RPC rate limit hit rate"
          description: "Rate limit hits: {{ $value | humanize }}/sec"

      # Alert on high RPC latency
      - alert: SolanaRPCLatencyHigh
        expr: histogram_quantile(0.95, rate(solana_rpc_call_duration_seconds_bucket[5m])) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High Solana RPC latency (p95 > 5s)"
          description: "p95 latency: {{ $value | humanize }}s"

      # Alert on low deduplication efficiency (may indicate polling too frequently)
      - alert: TransactionDeduplicationLow
        expr: transactions_deduplication_ratio < 0.5
        for: 10m
        labels:
          severity: info
        annotations:
          summary: "Low transaction deduplication ratio"
          description: "Wallet {{ $labels.wallet_address }} has low dedup ratio: {{ $value | humanizePercentage }}"

      # Alert on high error rate
      - alert: SolanaRPCErrorRateHigh
        expr: |
          rate(solana_rpc_calls_total{status="error"}[5m])
          /
          rate(solana_rpc_calls_total[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High Solana RPC error rate (>10%)"
          description: "Error rate: {{ $value | humanizePercentage }}"
```

---

## Performance Impact

### Expected Overhead

- **Counter increment**: ~10ns per call (negligible)
- **Histogram observation**: ~100ns per call (negligible)
- **Memory**: ~1KB per unique label combination
- **CPU**: <0.1% for typical load (10 wallets, 30s poll interval)

### Mitigation Strategies

1. **Label Cardinality**: Avoid high-cardinality labels (e.g., transaction signatures)
   - ✅ Good: `wallet_address` (bounded by number of registered wallets)
   - ❌ Bad: `transaction_signature` (unbounded, grows infinitely)

2. **Sampling**: For very high-volume operations, consider sampling
   - Example: Record only 1% of parse successes, but 100% of errors

3. **Buffering**: Prometheus client already buffers metrics in-memory

---

## Success Criteria

After implementation, we should be able to answer:

1. **RPC Usage Questions**:
   - How many RPC calls are we making per minute/hour?
   - What's the distribution of GetSignaturesForAddress vs GetTransaction calls?
   - Are we hitting rate limits? How often?
   - What's the average/p95/p99 latency for RPC calls?

2. **Efficiency Questions**:
   - What's our deduplication ratio? (Should be >80% for active wallets)
   - How many transactions are we fetching vs. writing?
   - Are we parsing transactions successfully?

3. **Performance Questions**:
   - How long does a typical poll workflow take?
   - Which activity takes the longest? (DB query, RPC fetch, or write?)
   - Are database queries fast enough? (Should be <10ms for signature lookups)

4. **Reliability Questions**:
   - What's the success rate for RPC calls? (Should be >99%)
   - What's the success rate for database writes? (Should be >99.9%)
   - Are workflows completing successfully?

---

## Future Enhancements

### Phase 8 (Optional): Distributed Tracing

Integrate OpenTelemetry for distributed tracing:
- Trace RPC calls through the entire workflow
- Correlate logs, metrics, and traces
- Visualize transaction flow: Solana → Worker → DB → NATS → SSE → Client

### Phase 9 (Optional): Custom Metrics Dashboards

Build internal dashboards for:
- Cost tracking (RPC calls × cost per call)
- Wallet-specific health scores
- Predictive alerts (e.g., "wallet will hit rate limit in 10min at current rate")

### Phase 10 (Optional): Metrics-Driven Optimization

Use metrics to drive optimizations:
- Adaptive polling intervals based on transaction frequency
- Batching strategies for high-volume wallets
- Circuit breaker patterns for failing RPC endpoints

---

## Summary

This implementation plan adds comprehensive Prometheus monitoring to forohtoo with:

- **Zero redundant RPC calls** (already achieved, now visible via metrics)
- **Full observability** into RPC usage, deduplication, and performance
- **Explicit dependency injection** following project patterns
- **Minimal performance overhead** (<0.1% CPU)
- **Production-ready alerting** for rate limits and errors

**Estimated Implementation Time**: 2-3 days
- Day 1: Core infrastructure (metrics package, middleware)
- Day 2: Instrumentation (Solana client, activities, handlers)
- Day 3: Testing, documentation, Grafana dashboards

**Priority Order**:
1. Phase 1-2: Solana RPC metrics (addresses user's primary concern)
2. Phase 3: Transaction processing metrics (deduplication visibility)
3. Phase 4-5: HTTP and workflow metrics (system health)
4. Phase 6-7: Database and NATS metrics (nice-to-have)
