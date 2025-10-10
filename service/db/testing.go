package db

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestStore wraps a Store with test cleanup functionality.
type TestStore struct {
	*Store
	pool *pgxpool.Pool
}

// NewTestStore creates a new Store connected to the test database.
// It reads the TEST_DATABASE_URL environment variable, or falls back to a default.
// The test database should be isolated from the development database.
func NewTestStore(t *testing.T) *TestStore {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Verify connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("failed to ping test database: %v", err)
	}

	store := NewStore(pool)

	return &TestStore{
		Store: store,
		pool:  pool,
	}
}

// Close closes the database connection pool.
func (ts *TestStore) Close() {
	ts.pool.Close()
}

// Cleanup removes all data from test tables.
// Call this in tests to ensure clean state between test cases.
func (ts *TestStore) Cleanup(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	_, err := ts.pool.Exec(ctx, "TRUNCATE TABLE transactions, wallets CASCADE")
	if err != nil {
		t.Fatalf("failed to cleanup test database: %v", err)
	}
}

// MustExec executes a SQL statement and fails the test if it errors.
// Useful for setting up test fixtures.
func (ts *TestStore) MustExec(t *testing.T, query string, args ...interface{}) {
	t.Helper()

	ctx := context.Background()
	_, err := ts.pool.Exec(ctx, query, args...)
	if err != nil {
		t.Fatalf("failed to execute query: %v\nQuery: %s", err, query)
	}
}

// SkipIfNoTestDB skips the test if the test database is not available.
// This is useful for running unit tests without requiring a database.
func SkipIfNoTestDB(t *testing.T) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	// Quick connection test
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("Skipping database test: cannot connect to test database: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Skipping database test: cannot ping test database: %v", err)
	}

	pool.Close()
}

// SetupTestDatabase ensures the test database schema is up to date.
// This should be called once before running tests, typically in TestMain.
func SetupTestDatabase() error {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		return fmt.Errorf("failed to connect to test database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		return fmt.Errorf("failed to ping test database: %w", err)
	}

	return nil
}
