package server_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/server"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerIntegration tests the full request/response cycle with a real server.
func TestServerIntegration(t *testing.T) {
	// Skip if no test database
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping integration test (SKIP_DB_TESTS is set)")
	}

	// Setup test database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(context.Background()))

	// Clean up database
	_, err = pool.Exec(context.Background(), "TRUNCATE TABLE transactions, wallets CASCADE")
	require.NoError(t, err)

	store := db.NewStore(pool)
	scheduler := temporal.NewMockScheduler()

	// Create test server on random port
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := server.New(":0", store, scheduler, nil, logger) // :0 assigns random available port, nil for SSE

	// Start server in background
	serverAddr := make(chan string, 1)
	serverErrors := make(chan error, 1)
	go func() {
		// We need to get the actual address after the server starts
		// For now, use a fixed test port
		testAddr := "localhost:18080"
		srv = server.New(testAddr, store, scheduler, nil, logger)
		serverAddr <- testAddr
		serverErrors <- srv.Start()
	}()

	// Wait for server to start
	var baseURL string
	select {
	case addr := <-serverAddr:
		baseURL = "http://" + addr
		time.Sleep(100 * time.Millisecond) // Give server time to start
	case err := <-serverErrors:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to start")
	}

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	// Create client
	httpClient := &http.Client{Timeout: 5 * time.Second}
	c := client.NewClient(baseURL, httpClient, logger)

	ctx := context.Background()

	// Test 1: Register a wallet
	t.Run("register wallet", func(t *testing.T) {
		err := c.Register(ctx, "TestWa11et11111111111111111111111111", 30*time.Second)
		require.NoError(t, err)
	})

	// Test 2: Get wallet
	t.Run("get wallet", func(t *testing.T) {
		wallet, err := c.Get(ctx, "TestWa11et11111111111111111111111111")
		require.NoError(t, err)
		assert.Equal(t, "TestWa11et11111111111111111111111111", wallet.Address)
		assert.Equal(t, 30*time.Second, wallet.PollInterval)
		assert.Equal(t, "active", wallet.Status)
	})

	// Test 3: List wallets
	t.Run("list wallets", func(t *testing.T) {
		// Register another wallet
		err := c.Register(ctx, "TestWa11et22222222222222222222222222", 60*time.Second)
		require.NoError(t, err)

		wallets, err := c.List(ctx)
		require.NoError(t, err)
		require.Len(t, wallets, 2)

		// Check both wallets are present
		addresses := []string{wallets[0].Address, wallets[1].Address}
		assert.Contains(t, addresses, "TestWa11et11111111111111111111111111")
		assert.Contains(t, addresses, "TestWa11et22222222222222222222222222")
	})

	// Test 4: Get non-existent wallet
	t.Run("get non-existent wallet", func(t *testing.T) {
		wallet, err := c.Get(ctx, "Nn5xistentWa11et333333333333333333")
		require.Error(t, err)
		assert.Nil(t, wallet)
		assert.Contains(t, err.Error(), "wallet not found")
	})

	// Test 5: Unregister wallet
	t.Run("unregister wallet", func(t *testing.T) {
		err := c.Unregister(ctx, "TestWa11et11111111111111111111111111")
		require.NoError(t, err)

		// Verify it's gone
		wallet, err := c.Get(ctx, "TestWa11et11111111111111111111111111")
		require.Error(t, err)
		assert.Nil(t, wallet)
	})

	// Test 6: Register with valid poll interval
	t.Run("register with valid poll interval", func(t *testing.T) {
		err := c.Register(ctx, "TestWa11et33333333333333333333333333", 5*time.Minute)
		require.NoError(t, err)
	})

	// Test 7: Duplicate registration
	t.Run("duplicate registration", func(t *testing.T) {
		err := c.Register(ctx, "TestWa11et22222222222222222222222222", 30*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to register wallet")
	})

	// Test 8: Unregister non-existent wallet
	t.Run("unregister non-existent wallet", func(t *testing.T) {
		err := c.Unregister(ctx, "Nn5xistentWa11et444444444444444444")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wallet not found")
	})
}

// TestHealthEndpoint tests the health check endpoint.
func TestHealthEndpoint(t *testing.T) {
	// Skip if no test database
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping integration test (SKIP_DB_TESTS is set)")
	}

	// Setup minimal server (health check doesn't need database)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := db.NewStore(pool)
	scheduler := temporal.NewMockScheduler()

	testAddr := "localhost:18081"
	srv := server.New(testAddr, store, scheduler, nil, logger)

	// Start server
	go srv.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	time.Sleep(100 * time.Millisecond) // Wait for server to start

	// Test health endpoint
	resp, err := http.Get(fmt.Sprintf("http://%s/health", testAddr))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
