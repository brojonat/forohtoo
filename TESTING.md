# Testing Guide

This document describes how to run tests for the forohtoo project.

## Unit Tests

Unit tests run without external dependencies and always run by default:

```bash
go test ./...
```

## Integration Tests

Integration tests require external services (database, Temporal) and are **opt-in** by default. They will be skipped unless explicitly enabled.

### Database Integration Tests

Database tests require a PostgreSQL/TimescaleDB instance.

**Using Docker Compose (recommended):**

```bash
# Start test database
docker-compose up -d postgres-test

# Wait for database to be ready
sleep 5

# Run migrations
make migrate-test  # or manually run migrations against test DB

# Run database tests
RUN_DB_TESTS=1 go test -v ./cmd/forohtoo -run TestList
RUN_DB_TESTS=1 go test -v ./cmd/forohtoo -run TestGet
RUN_DB_TESTS=1 go test -v ./cmd/forohtoo -run TestTransaction

# Or run all DB tests
RUN_DB_TESTS=1 go test -v ./cmd/forohtoo
```

**Environment Variables:**
- `RUN_DB_TESTS=1` - Enable database integration tests
- `TEST_DATABASE_URL` - Override default test database URL (default: `postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable`)

### Temporal Integration Tests

Temporal tests require a running Temporal server.

**Using Docker Compose (recommended):**

```bash
# Start Temporal and its dependencies
docker-compose up -d postgres temporal temporal-ui

# Wait for Temporal to be ready (can take 30-60 seconds)
sleep 60

# Run Temporal tests
RUN_TEMPORAL_TESTS=1 go test -v ./cmd/forohtoo -run TestTemporal

# Or run specific Temporal test
RUN_TEMPORAL_TESTS=1 go test -v ./cmd/forohtoo -run TestPauseSchedule
```

**Environment Variables:**
- `RUN_TEMPORAL_TESTS=1` - Enable Temporal integration tests
- `TEST_TEMPORAL_HOST` - Override Temporal host (default: `localhost:7233`)
- `TEST_TEMPORAL_NAMESPACE` - Override Temporal namespace (default: `default`)

**Temporal UI:**
When running via docker-compose, the Temporal UI is available at http://localhost:8080

### Running All Integration Tests

```bash
# Start all services
docker-compose up -d

# Wait for services to be ready
sleep 60

# Run all tests including integration tests
RUN_DB_TESTS=1 RUN_TEMPORAL_TESTS=1 go test -v ./...
```

## CI/CD Considerations

In CI environments, integration tests should be run in separate jobs:

```yaml
# Example GitHub Actions workflow
jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Run unit tests
        run: go test ./...

  integration-tests:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: timescale/timescaledb:latest-pg16
        # ... config
      temporal:
        image: temporalio/auto-setup:latest
        # ... config
    steps:
      - uses: actions/checkout@v3
      - name: Run integration tests
        run: |
          RUN_DB_TESTS=1 RUN_TEMPORAL_TESTS=1 go test -v ./...
```

## Test Organization

### CLI Tests

Located in `cmd/forohtoo/*_test.go`:

- `db_commands_test.go` - Database inspection commands (requires DB)
- `temporal_commands_test.go` - Temporal schedule management commands (requires Temporal)
- `server_commands_test.go` - Server health and version commands (no external deps)

### Test Patterns

Integration tests follow this pattern:

```go
func setupTestDB(t *testing.T) *db.Store {
    t.Helper()

    // Skip by default - require explicit opt-in
    if os.Getenv("RUN_DB_TESTS") == "" {
        t.Skip("Skipping database integration test (set RUN_DB_TESTS=1 to enable)")
    }

    // ... test setup
}
```

This ensures:
- Tests skip cleanly by default
- Clear error message when skipped
- Easy to enable for local development or CI
- No false failures from missing services

## Troubleshooting

### Database tests fail with "connection refused"

Ensure postgres-test container is running:
```bash
docker-compose ps postgres-test
docker-compose logs postgres-test
```

### Temporal tests fail with "connection refused"

Temporal takes 30-60 seconds to fully start. Wait longer or check logs:
```bash
docker-compose ps temporal
docker-compose logs temporal
```

### Tests timeout

Increase timeout for slower CI environments:
```bash
RUN_DB_TESTS=1 go test -v -timeout 5m ./...
```

## Best Practices

1. **Always skip integration tests by default** - Use env var opt-in pattern
2. **Clean up after tests** - Use `t.Cleanup()` to ensure resources are freed
3. **Use test-specific databases/namespaces** - Avoid conflicts with development environments
4. **Document environment variables** - Make it easy for others to run tests
5. **Fast local development** - Use docker-compose for quick service startup
