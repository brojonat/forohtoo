# Go Code Patterns and Examples

This document contains concrete code examples and patterns referenced in CLAUDE.md. These patterns represent best practices for writing clear, maintainable Go code.

## Test-Driven Development

```go
// 1. Write the test (it will fail)
func TestWalletPoller_PollNewTransactions(t *testing.T) {
    // Arrange
    mockClient := &MockSolanaClient{}
    poller := NewWalletPoller(mockClient)

    // Act
    txns, err := poller.Poll(ctx, walletAddress)

    // Assert
    require.NoError(t, err)
    assert.Len(t, txns, 5)
}

// 2. Write minimal code to pass the test
// 3. Refactor while keeping tests green
```

## Server + Client Development

```go
// Server implementation
func (s *Server) HandleAddWallet(req *AddWalletRequest) error {
    // ...
}

// Client method (in client package)
func (c *Client) AddWallet(ctx context.Context, address string, interval time.Duration) error {
    req := &AddWalletRequest{Address: address, PollInterval: interval}
    return c.request(ctx, "wallet.add", req)
}

// Test that exercises both
func TestAddWallet_EndToEnd(t *testing.T) {
    // ...
}
```

## Test Types

```go
// Unit test: Fast, isolated, no external dependencies
func TestParseTransactionMemo(t *testing.T) {
    memo := `{"workflow_id": "abc123"}`
    result, err := ParseMemo(memo)
    require.NoError(t, err)
    assert.Equal(t, "abc123", result.WorkflowID)
}

// Integration test: Uses real components (DB, NATS)
// +build integration
func TestWalletPoller_WithRealDatabase(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    // ...
}

// E2E test: Full system test
func TestPaymentWorkflow_EndToEnd(t *testing.T) {
    // Start server, client, make real calls
}
```

## Error Handling

```go
// Bad
txns, _ := client.GetTransactions(ctx)

// Good
txns, err := client.GetTransactions(ctx)
if err != nil {
    return fmt.Errorf("failed to get transactions: %w", err)
}

// Structured errors
type WalletNotFoundError struct {
    Address string
}

func (e *WalletNotFoundError) Error() string {
    return fmt.Sprintf("wallet not found: %s", e.Address)
}
```

## Explicit Dependencies

```go
// Bad: Hidden dependency
var db *sql.DB

func SaveTransaction(txn *Transaction) error {
    _, err := db.Exec("INSERT INTO transactions ...")
    return err
}

// Good: Explicit dependency
func SaveTransaction(ctx context.Context, db *sql.DB, txn *Transaction) error {
    _, err := db.ExecContext(ctx, "INSERT INTO transactions ...")
    return err
}

// Best: Constructor injection with interfaces
type TransactionStore interface {
    Save(ctx context.Context, txn *Transaction) error
}

type PostgresStore struct {
    db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
    return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, txn *Transaction) error {
    _, err := s.db.ExecContext(ctx, "INSERT INTO transactions ...")
    return err
}

// Usage
store := NewPostgresStore(db)
poller := NewPoller(client, store, conn)
server := NewServer(poller, store, conn)
```

## Server Structure

```go
type Server struct {
    poller  *Poller
    store   TransactionStore
    conn    *nats.Conn
    logger  *slog.Logger
}

func NewServer(
    poller *Poller,
    store TransactionStore,
    conn *nats.Conn,
    logger *slog.Logger,
) *Server {
    return &Server{
        poller: poller,
        store:  store,
        conn:   conn,
        logger: logger,
    }
}
```

## HTTP Handler Pattern

```go
// Handler function takes dependencies and returns http.Handler
func handleListWallets(store TransactionStore, logger *slog.Logger) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()

        wallets, err := store.ListWallets(ctx)
        if err != nil {
            logger.ErrorContext(ctx, "failed to list wallets", "error", err)
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(wallets)
    })
}
```

## Middleware Pattern

```go
// adapter wraps a handler and returns a new handler
type adapter func(http.Handler) http.Handler

// adaptHandler applies adapters in reverse order
// so the first adapter in the list is the outermost (called first)
func adaptHandler(h http.Handler, adapters ...adapter) http.Handler {
    for i := len(adapters) - 1; i >= 0; i-- {
        h = adapters[i](h)
    }
    return h
}

// Logging adapter
func withLogging(logger *slog.Logger) adapter {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            logger.InfoContext(r.Context(), "request started",
                "method", r.Method,
                "path", r.URL.Path,
            )

            next.ServeHTTP(w, r)

            logger.InfoContext(r.Context(), "request completed",
                "method", r.Method,
                "path", r.URL.Path,
                "duration", time.Since(start),
            )
        })
    }
}

// JWT Authentication adapter
func withJWTAuth(secret []byte) adapter {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                http.Error(w, "missing authorization header", http.StatusUnauthorized)
                return
            }

            tokenString := strings.TrimPrefix(authHeader, "Bearer ")
            if tokenString == authHeader {
                http.Error(w, "invalid authorization format", http.StatusUnauthorized)
                return
            }

            token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
                if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
                }
                return secret, nil
            })

            if err != nil || !token.Valid {
                http.Error(w, "invalid token", http.StatusUnauthorized)
                return
            }

            if claims, ok := token.Claims.(jwt.MapClaims); ok {
                ctx := context.WithValue(r.Context(), "user_id", claims["sub"])
                ctx = context.WithValue(ctx, "claims", claims)
                next.ServeHTTP(w, r.WithContext(ctx))
                return
            }

            http.Error(w, "invalid token claims", http.StatusUnauthorized)
        })
    }
}

// Request ID adapter
func withRequestID() adapter {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            requestID := uuid.New().String()
            ctx := context.WithValue(r.Context(), "request_id", requestID)
            w.Header().Set("X-Request-ID", requestID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Prometheus metrics adapter
func withMetrics(registry *prometheus.Registry) adapter {
    httpDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
        Name:    "http_request_duration_seconds",
        Help:    "Duration of HTTP requests in seconds",
        Buckets: prometheus.DefBuckets,
    }, []string{"method", "path", "status"})

    httpRequestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "http_requests_total",
        Help: "Total number of HTTP requests",
    }, []string{"method", "path", "status"})

    registry.MustRegister(httpDuration, httpRequestsTotal)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

            next.ServeHTTP(wrapped, r)

            duration := time.Since(start).Seconds()
            status := strconv.Itoa(wrapped.statusCode)
            labels := prometheus.Labels{
                "method": r.Method,
                "path":   r.URL.Path,
                "status": status,
            }

            httpDuration.With(labels).Observe(duration)
            httpRequestsTotal.With(labels).Inc()
        })
    }
}
```

## HTTP Response Helpers

```go
func writeJSONResponse(w http.ResponseWriter, resp interface{}, code int) {
    w.WriteHeader(code)
    json.NewEncoder(w).Encode(resp)
}

func writeOK(w http.ResponseWriter) {
    resp := map[string]string{"message": "ok"}
    writeJSONResponse(w, resp, http.StatusOK)
}

func writeInternalError(l *slog.Logger, w http.ResponseWriter, e error) {
    l.Error("internal error", "error", e.Error())
    resp := map[string]string{"error": "internal error"}
    writeJSONResponse(w, resp, http.StatusInternalServerError)
}

func writeBadRequestError(l *slog.Logger, w http.ResponseWriter, err error) {
    l.Debug("bad request", "error", err.Error())
    resp := map[string]string{"error": err.Error()}
    writeJSONResponse(w, resp, http.StatusBadRequest)
}
```

## Health Check Handler

```go
func handleHealth(db *sql.DB) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()

        if err := db.PingContext(ctx); err != nil {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusServiceUnavailable)
            json.NewEncoder(w).Encode(map[string]interface{}{
                "status": "unhealthy",
                "error":  "database unavailable",
            })
            return
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]interface{}{
            "status": "healthy",
            "timestamp": time.Now().UTC().Format(time.RFC3339),
        })
    })
}
```

## Wiring Up Routes

```go
func main() {
    // Initialize dependencies
    db := setupDatabase()
    store := NewPostgresStore(db)
    logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

    jwtSecret := []byte(os.Getenv("JWT_SECRET"))
    if len(jwtSecret) == 0 {
        logger.Error("JWT_SECRET environment variable not set")
        os.Exit(1)
    }

    promRegistry := prometheus.NewRegistry()
    mux := http.NewServeMux()

    // Public endpoints
    mux.Handle("GET /health", adaptHandler(
        handleHealth(db),
        withRequestID(),
        withLogging(logger),
    ))

    mux.Handle("GET /metrics", promhttp.HandlerFor(promRegistry, promhttp.HandlerOpts{}))

    // Protected endpoints
    mux.Handle("GET /wallets", adaptHandler(
        handleListWallets(store, logger),
        withRequestID(),
        withLogging(logger),
        withMetrics(promRegistry),
        withJWTAuth(jwtSecret),
    ))

    addr := ":8080"
    logger.Info("starting server", "addr", addr)
    if err := http.ListenAndServe(addr, mux); err != nil {
        logger.Error("server failed", "error", err)
        os.Exit(1)
    }
}
```

## Handler Testing

```go
func TestHandleListWallets(t *testing.T) {
    // Arrange
    mockStore := &MockTransactionStore{
        wallets: []Wallet{{Address: "abc123"}},
    }
    logger := slog.New(slog.NewTextHandler(io.Discard, nil))

    handler := handleListWallets(mockStore, logger)
    req := httptest.NewRequest("GET", "/wallets", nil)
    rec := httptest.NewRecorder()

    // Act
    handler.ServeHTTP(rec, req)

    // Assert
    assert.Equal(t, http.StatusOK, rec.Code)

    var wallets []Wallet
    json.NewDecoder(rec.Body).Decode(&wallets)
    assert.Len(t, wallets, 1)
    assert.Equal(t, "abc123", wallets[0].Address)
}
```

## CLI with urfave/cli

```go
package main

import (
    "log"
    "os"

    "github.com/urfave/cli/v2"
)

func main() {
    app := &cli.App{
        Name:  "myapp",
        Usage: "My application",
        Commands: []*cli.Command{
            {
                Name:  "server",
                Usage: "Start the HTTP server",
                Flags: []cli.Flag{
                    &cli.StringFlag{
                        Name:    "addr",
                        Value:   ":8080",
                        Usage:   "HTTP server address",
                        EnvVars: []string{"SERVER_ADDR"},
                    },
                    &cli.StringFlag{
                        Name:     "db-url",
                        Usage:    "Database connection string",
                        EnvVars:  []string{"DATABASE_URL"},
                        Required: true,
                    },
                    &cli.StringFlag{
                        Name:    "log-level",
                        Value:   "warn",
                        Usage:   "Log level (debug, info, warn, error)",
                        EnvVars: []string{"LOG_LEVEL"},
                    },
                },
                Action: runServer,
            },
        },
    }

    if err := app.Run(os.Args); err != nil {
        log.Fatal(err)
    }
}

func runServer(c *cli.Context) error {
    addr := c.String("addr")
    dbURL := c.String("db-url")
    logLevel := parseLogLevel(c.String("log-level"))

    logger := setupLogger(logLevel)
    logger.Info("starting server", "addr", addr)

    // Initialize and run server...
    return nil
}
```

## Structured Logging with slog

```go
// Setup logger with configurable level
func setupLogger(level slog.Level) *slog.Logger {
    opts := &slog.HandlerOptions{
        Level: level,
    }
    return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// Usage in code - most logs at DEBUG level
logger.DebugContext(ctx, "processing request",
    "user_id", userID,
    "action", action,
)

// Only use INFO for significant lifecycle events
logger.InfoContext(ctx, "server started",
    "addr", addr,
    "version", version,
)

// WARN for recoverable issues
logger.WarnContext(ctx, "rate limit exceeded, retrying",
    "user_id", userID,
    "retry_after", retryAfter,
)

// ERROR for failures
logger.ErrorContext(ctx, "failed to store data",
    "error", err,
    "id", id,
)

// Control verbosity via environment variable
func main() {
    logLevel := slog.LevelWarn
    if level := os.Getenv("LOG_LEVEL"); level != "" {
        switch strings.ToUpper(level) {
        case "DEBUG":
            logLevel = slog.LevelDebug
        case "INFO":
            logLevel = slog.LevelInfo
        case "WARN":
            logLevel = slog.LevelWarn
        case "ERROR":
            logLevel = slog.LevelError
        }
    }

    logger := setupLogger(logLevel)
    // ...
}
```

## sqlc Configuration

**sqlc.yaml:**

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries:
      - "db/sqlc/users.sql"
      - "db/sqlc/orders.sql"
    schema: "db/sqlc/schema.sql"
    gen:
      go:
        package: "dbgen"
        out: "db/dbgen"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_interface: true
```

**SQL Queries (db/sqlc/users.sql):**

```sql
-- name: GetUser :one
SELECT * FROM users
WHERE id = $1 LIMIT 1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CreateUser :one
INSERT INTO users (
    email,
    name
) VALUES (
    $1, $2
)
RETURNING *;
```

**Usage:**

```go
package main

import (
    "context"
    "database/sql"

    "github.com/yourorg/myapp/internal/db"
)

func example(ctx context.Context, conn *sql.DB) error {
    queries := db.New(conn)

    // Type-safe queries with compile-time checking
    user, err := queries.GetUser(ctx, 123)
    if err != nil {
        return err
    }

    // Parameters are strongly typed
    users, err := queries.ListUsers(ctx, db.ListUsersParams{
        Limit:  10,
        Offset: 0,
    })
    if err != nil {
        return err
    }

    return nil
}
```

## Makefile Template

```makefile
.PHONY: test
test:
	go test ./... -v -race -cover

.PHONY: test-integration
test-integration:
	go test ./... -v -tags=integration

.PHONY: lint
lint:
	golangci-lint run

.PHONY: build-server
build-server:
	go build -o bin/server ./cmd/server

.PHONY: dev
dev:
	air

.PHONY: sqlc-generate
sqlc-generate:
	sqlc generate

.PHONY: sqlc-verify
sqlc-verify:
	sqlc verify

.PHONY: db-migrate
db-migrate:
	migrate -path migrations -database "${DATABASE_URL}" up

.PHONY: db-reset
db-reset:
	migrate -path migrations -database "${DATABASE_URL}" drop
	migrate -path migrations -database "${DATABASE_URL}" up

.PHONY: pre-commit
pre-commit: sqlc-verify test lint
```

## Air Configuration

**.air.toml:**

```toml
root = "."
tmp_dir = "tmp"

[build]
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ./cmd/server"
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "frontend"]
  include_ext = ["go", "tpl", "tmpl", "html"]
  exclude_regex = ["_test\\.go"]
```

## Development Logging with tee

```bash
# Create logs directory
mkdir -p logs

# Run server with tee to log to file
./server 2>&1 | tee -a logs/server.log

# Run with debug logging enabled
LOG_LEVEL=DEBUG ./server 2>&1 | tee -a logs/server-debug.log

# Rotate logs daily
./server 2>&1 | tee -a logs/server-$(date +%Y-%m-%d).log
```

## Querying Logs with jq

```bash
# Find all errors
jq 'select(.level == "ERROR")' logs/server.log

# Find slow requests (over 1 second)
jq 'select(.duration > 1.0)' logs/server.log

# Group errors by type
jq -r 'select(.level == "ERROR") | .error' logs/server.log | sort | uniq -c

# Find all logs for a specific request ID
jq 'select(.request_id == "abc-123")' logs/server.log

# Monitor logs in real-time
tail -f logs/server.log | jq 'select(.level == "ERROR" or .level == "WARN")'
```
