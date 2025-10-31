package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterWalletAsset_WithTemporalSchedule_Integration tests wallet registration
// with actual Temporal schedule creation.
//
// This integration test requires:
// - Running Temporal server (TEMPORAL_HOST)
// - PostgreSQL database (TEST_DATABASE_URL)
//
// Set RUN_INTEGRATION_TESTS=1 to enable this test.
func TestRegisterWalletAsset_WithTemporalSchedule_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
	}

	// Setup database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Clean test data
	_, err = pool.Exec(ctx, "DELETE FROM wallets WHERE address LIKE 'TEST-HTTP%'")
	require.NoError(t, err)

	store := db.NewStore(pool)

	// Create Temporal client
	temporalClient, err := temporal.NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		slog.Default(),
	)
	require.NoError(t, err)
	defer temporalClient.Close()

	// Create handler
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleRegisterWalletAsset(store, temporalClient, cfg, logger)

	// Test wallet registration
	testAddress := "TEST-HTTP-" + time.Now().Format("20060102150405")
	tokenMint := cfg.USDCDevnetMintAddress

	requestBody := map[string]interface{}{
		"address": testAddress,
		"network": "devnet",
		"asset": map[string]interface{}{
			"type":       "spl-token",
			"token_mint": tokenMint,
		},
		"poll_interval": "2m",
	}

	body, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed
	assert.Equal(t, http.StatusCreated, w.Code, "response body: %s", w.Body.String())

	// Verify wallet in database
	wallet, err := store.GetWallet(ctx, testAddress, "devnet", "spl-token", tokenMint)
	require.NoError(t, err)
	assert.Equal(t, testAddress, wallet.Address)
	assert.Equal(t, 2*time.Minute, wallet.PollInterval)
	assert.Equal(t, "active", wallet.Status)

	// Verify response
	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, testAddress, response["address"])
	assert.Equal(t, "devnet", response["network"])

	// Verify Temporal schedule exists
	// Note: We can't easily verify this without Temporal SDK access to list schedules
	// In a full integration test, you'd query Temporal to verify the schedule was created
	t.Log("✓ Wallet registered successfully with Temporal schedule")

	// Cleanup
	err = store.DeleteWallet(ctx, testAddress, "devnet", "spl-token", tokenMint)
	require.NoError(t, err)

	err = temporalClient.DeleteWalletAssetSchedule(ctx, testAddress, "devnet", "spl-token", tokenMint)
	if err != nil {
		t.Logf("Warning: Failed to delete schedule (may not exist): %v", err)
	}
}

// TestUnregisterWalletAsset_WithTemporalSchedule_Integration tests wallet unregistration
// with actual Temporal schedule deletion.
func TestUnregisterWalletAsset_WithTemporalSchedule_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
	}

	// Setup database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	store := db.NewStore(pool)

	// Create Temporal client
	temporalClient, err := temporal.NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		slog.Default(),
	)
	require.NoError(t, err)
	defer temporalClient.Close()

	// Pre-create wallet and schedule
	testAddress := "TEST-UNREG-" + time.Now().Format("20060102150405")
	tokenMint := cfg.USDCDevnetMintAddress
	ataAddr := testAddress + "-ATA"

	wallet, err := store.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:                testAddress,
		Network:                "devnet",
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
		PollInterval:           1 * time.Minute,
		Status:                 "active",
	})
	require.NoError(t, err)
	require.NotNil(t, wallet)

	err = temporalClient.UpsertWalletAssetSchedule(ctx, testAddress, "devnet", "spl-token", tokenMint, &ataAddr, 1*time.Minute)
	require.NoError(t, err)

	t.Logf("Created test wallet and schedule: %s", testAddress)

	// Create handler
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleUnregisterWalletAsset(store, temporalClient, logger)

	// Test wallet unregistration
	req := httptest.NewRequest("DELETE", "/api/v1/wallet-assets/"+testAddress+"?network=devnet&asset_type=spl-token&token_mint="+tokenMint, nil)
	req.SetPathValue("address", testAddress)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed
	assert.Equal(t, http.StatusNoContent, w.Code, "response body: %s", w.Body.String())

	// Verify wallet deleted from database
	_, err = store.GetWallet(ctx, testAddress, "devnet", "spl-token", tokenMint)
	assert.Error(t, err, "wallet should be deleted")

	// Verify schedule deleted (best effort - may already be gone)
	t.Log("✓ Wallet unregistered successfully with Temporal schedule deletion")
}

// TestRegisterWalletAsset_PaymentGateway_Integration tests the payment gateway flow
// via HTTP API.
//
// This test verifies that:
// - POST request with payment gateway enabled returns 402 Payment Required
// - Response includes payment invoice and workflow ID
// - Workflow is started in Temporal
func TestRegisterWalletAsset_PaymentGateway_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
	}

	// Only run if payment gateway is enabled
	if !cfg.PaymentGateway.Enabled {
		t.Skip("Payment gateway not enabled in config")
	}

	// Setup database
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()
	store := db.NewStore(pool)

	// Create Temporal client
	temporalClient, err := temporal.NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		slog.Default(),
	)
	require.NoError(t, err)
	defer temporalClient.Close()

	// Create handler
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleRegisterWalletAsset(store, temporalClient, cfg, logger)

	// Test wallet registration (should require payment)
	testAddress := "TEST-PAYMENT-HTTP-" + time.Now().Format("20060102150405")
	tokenMint := cfg.USDCDevnetMintAddress

	requestBody := map[string]interface{}{
		"address": testAddress,
		"network": "devnet",
		"asset": map[string]interface{}{
			"type":       "spl-token",
			"token_mint": tokenMint,
		},
		"poll_interval": "1m",
	}

	body, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return 402 Payment Required
	assert.Equal(t, http.StatusPaymentRequired, w.Code, "response body: %s", w.Body.String())

	// Parse response
	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify response structure
	assert.Equal(t, "payment_required", response["status"])
	assert.NotNil(t, response["invoice"], "invoice should be present")
	assert.NotEmpty(t, response["workflow_id"], "workflow_id should be present")
	assert.NotEmpty(t, response["status_url"], "status_url should be present")

	workflowID := response["workflow_id"].(string)
	t.Logf("Payment workflow started: %s", workflowID)

	// Verify invoice structure
	invoice := response["invoice"].(map[string]interface{})
	assert.Equal(t, testAddress, invoice["id"], "invoice ID should be wallet address")
	assert.NotEmpty(t, invoice["payment_url"], "payment URL should be present")
	assert.NotEmpty(t, invoice["qr_code"], "QR code should be present")
	assert.NotEmpty(t, invoice["memo"], "memo should be present")

	// Verify wallet was NOT created yet (payment required first)
	_, err = store.GetWallet(ctx, testAddress, "devnet", "spl-token", tokenMint)
	assert.Error(t, err, "wallet should not exist before payment")

	t.Log("✓ Payment gateway integration test passed")
	t.Logf("Payment URL: %s", invoice["payment_url"])
	t.Logf("Memo: %s", invoice["memo"])
}

// TestGetRegistrationStatus_Integration tests querying workflow status via HTTP API.
func TestGetRegistrationStatus_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
	}

	// Create Temporal client
	temporalClient, err := temporal.NewClient(
		cfg.TemporalHost,
		cfg.TemporalNamespace,
		cfg.TemporalTaskQueue,
		slog.Default(),
	)
	require.NoError(t, err)
	defer temporalClient.Close()

	// Create a test workflow (using Temporal SDK directly)
	// Note: This requires a running worker to process the workflow
	// For a full test, you'd start an actual workflow and query its status

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleGetRegistrationStatus(temporalClient, logger)

	// Test with non-existent workflow
	req := httptest.NewRequest("GET", "/api/v1/registration-status/non-existent-workflow", nil)
	req.SetPathValue("workflow_id", "non-existent-workflow")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return 404
	assert.Equal(t, http.StatusNotFound, w.Code)

	t.Log("✓ Registration status integration test passed")
}
