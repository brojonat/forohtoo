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
	"github.com/brojonat/forohtoo/service/config"
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
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
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
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}

	// Create test server on random port
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := server.New(":0", cfg, store, scheduler, nil, nil, logger) // :0 assigns random available port, nil for SSE and metrics

	// Start server in background
	serverAddr := make(chan string, 1)
	serverErrors := make(chan error, 1)
	go func() {
		// We need to get the actual address after the server starts
		// For now, use a fixed test port
		testAddr := "localhost:18080"
		srv = server.New(testAddr, cfg, store, scheduler, nil, nil, logger) // nil for SSE and metrics
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
		err := c.RegisterAsset(ctx, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "mainnet", "sol", "", 60*time.Second)
		require.NoError(t, err)
	})

	// Test 2: Verify wallet exists in list (Get endpoint doesn't support asset-aware wallets)
	t.Run("get wallet", func(t *testing.T) {
		wallets, err := c.List(ctx)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(wallets), 1)

		// Find the wallet we just registered
		var found bool
		for _, w := range wallets {
			if w.Address == "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA" && w.Network == "mainnet" {
				assert.Equal(t, 60*time.Second, w.PollInterval)
				assert.Equal(t, "active", w.Status)
				found = true
				break
			}
		}
		assert.True(t, found, "registered wallet should be in list")
	})

	// Test 3: List wallets
	t.Run("list wallets", func(t *testing.T) {
		// Register another wallet
		err := c.RegisterAsset(ctx, "SysvarRent111111111111111111111111111111111", "mainnet", "sol", "", 60*time.Second)
		require.NoError(t, err)

		wallets, err := c.List(ctx)
		require.NoError(t, err)
		require.Len(t, wallets, 2)

		// Check both wallets are present
		addresses := []string{wallets[0].Address, wallets[1].Address}
		assert.Contains(t, addresses, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
		assert.Contains(t, addresses, "SysvarRent111111111111111111111111111111111")
	})

	// Test 4: Get non-existent wallet
	t.Run("get non-existent wallet", func(t *testing.T) {
		wallet, err := c.Get(ctx, "Nn5xistentWa11et333333333333333333", "mainnet")
		require.Error(t, err)
		assert.Nil(t, wallet)
		assert.Contains(t, err.Error(), "wallet not found")
	})

	// Test 5: Unregister wallet
	t.Run("unregister wallet", func(t *testing.T) {
		err := c.UnregisterAsset(ctx, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "mainnet", "sol", "")
		require.NoError(t, err)

		// Verify it's gone
		wallet, err := c.Get(ctx, "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "mainnet")
		require.Error(t, err)
		assert.Nil(t, wallet)
	})

	// Test 6: Register with valid poll interval
	t.Run("register with valid poll interval", func(t *testing.T) {
		err := c.RegisterAsset(ctx, "SysvarC1ock11111111111111111111111111111111", "mainnet", "sol", "", 5*time.Minute)
		require.NoError(t, err)
	})

	// Test 7: Duplicate registration (upsert behavior - should succeed)
	t.Run("duplicate registration", func(t *testing.T) {
		// Re-registering should succeed (upsert behavior) with updated interval
		err := c.RegisterAsset(ctx, "SysvarRent111111111111111111111111111111111", "mainnet", "sol", "", 120*time.Second)
		require.NoError(t, err)

		// Verify the interval was updated
		wallets, err := c.List(ctx)
		require.NoError(t, err)
		var found bool
		for _, w := range wallets {
			if w.Address == "SysvarRent111111111111111111111111111111111" && w.Network == "mainnet" {
				assert.Equal(t, 120*time.Second, w.PollInterval)
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	// Test 8: Unregister non-existent wallet
	t.Run("unregister non-existent wallet", func(t *testing.T) {
		err := c.UnregisterAsset(ctx, "Nn5xistentWa11et444444444444444444", "mainnet", "sol", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
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
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := db.NewStore(pool)
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}

	testAddr := "localhost:18081"
	srv := server.New(testAddr, cfg, store, scheduler, nil, nil, logger) // nil for SSE and metrics

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
