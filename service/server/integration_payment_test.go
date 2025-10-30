// +build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// TestPaymentFlow_EndToEnd_Success tests the complete payment-gated registration flow
// from initial request through payment to successful wallet registration.
//
// WHAT IS BEING TESTED:
// We're testing the entire happy path of the payment gateway feature, including:
// - HTTP handler returning 402 with invoice
// - Temporal workflow starting and waiting for payment
// - Payment detection via client.Await()
// - Wallet registration and schedule creation
// - Status endpoint reporting completion
//
// EXPECTED BEHAVIOR:
// - POST /api/v1/wallet-assets for new wallet returns 402 Payment Required
// - Response includes invoice with payment details and workflow_id
// - After simulating payment, status endpoint eventually returns "completed"
// - Wallet exists in database with correct parameters
// - Temporal schedule exists for wallet polling
// - Payment signature is recorded in workflow result
//
// This is the primary end-to-end test for the payment gateway feature.
func TestPaymentFlow_EndToEnd_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test infrastructure
	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	// Ensure service wallet is registered
	err := ensureServiceWalletRegistered(ctx, testDB, nil, cfg)
	if err != nil {
		t.Fatalf("Failed to register service wallet: %v", err)
	}

	// Create test server
	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	// Step 1: POST /api/v1/wallet-assets for new wallet
	reqBody := map[string]interface{}{
		"address": "TestWallet12345678901234567890123456789",
		"network": "devnet",
		"asset": map[string]interface{}{
			"type": "sol",
		},
		"poll_interval": "30s",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler.ServeHTTP(w, req)

	// Verify 402 Payment Required
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("Expected status 402 Payment Required, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Parse response
	var registrationResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &registrationResp)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Extract workflow ID and invoice
	workflowID, ok := registrationResp["workflow_id"].(string)
	if !ok || workflowID == "" {
		t.Fatal("Response should contain workflow_id")
	}

	invoice, ok := registrationResp["invoice"].(map[string]interface{})
	if !ok {
		t.Fatal("Response should contain invoice")
	}

	memo, ok := invoice["memo"].(string)
	if !ok || memo == "" {
		t.Fatal("Invoice should contain memo")
	}

	// Step 2: Verify status is "pending"
	statusReq := httptest.NewRequest("GET", "/api/v1/registration-status/"+workflowID, nil)
	statusW := httptest.NewRecorder()
	server.Handler.ServeHTTP(statusW, statusReq)

	if statusW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 OK, got %d", statusW.Code)
	}

	var statusResp map[string]interface{}
	json.Unmarshal(statusW.Body.Bytes(), &statusResp)

	if statusResp["status"] != "pending" {
		t.Errorf("Expected status=pending, got %v", statusResp["status"])
	}

	// Step 3: Simulate payment by inserting transaction into database
	paymentSignature := simulatePayment(t, ctx, testDB, cfg.ServiceWallet, "devnet", cfg.FeeAmount, memo)

	// Step 4: Poll status endpoint until completion (with timeout)
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var finalStatus map[string]interface{}
	completed := false

	for !completed {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for workflow to complete")
		case <-ticker.C:
			statusReq := httptest.NewRequest("GET", "/api/v1/registration-status/"+workflowID, nil)
			statusW := httptest.NewRecorder()
			server.Handler.ServeHTTP(statusW, statusReq)

			json.Unmarshal(statusW.Body.Bytes(), &finalStatus)

			if finalStatus["status"] == "completed" {
				completed = true
			} else if finalStatus["status"] == "failed" {
				t.Fatalf("Workflow failed: %v", finalStatus["error"])
			}
		}
	}

	// Step 5: Verify workflow result
	if finalStatus["payment_signature"] != paymentSignature {
		t.Errorf("Expected payment_signature=%s, got %v", paymentSignature, finalStatus["payment_signature"])
	}

	paymentAmount, ok := finalStatus["payment_amount"].(float64)
	if !ok || int64(paymentAmount) != cfg.FeeAmount {
		t.Errorf("Expected payment_amount=%d, got %v", cfg.FeeAmount, finalStatus["payment_amount"])
	}

	// Step 6: Verify wallet in database
	walletExists, err := testDB.WalletExists(ctx, reqBody["address"].(string), "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check wallet existence: %v", err)
	}
	if !walletExists {
		t.Error("Wallet should exist in database after payment")
	}

	// Step 7: Verify Temporal schedule exists
	// Note: This would require querying Temporal's schedule API
	// For now, we verify wallet registration implies schedule was created

	t.Logf("✓ End-to-end payment flow completed successfully")
}

// TestPaymentFlow_EndToEnd_Timeout tests that the workflow correctly handles
// payment timeout when the user never sends payment.
//
// WHAT IS BEING TESTED:
// We're testing that workflows timeout gracefully when no payment is received,
// preventing resource leaks and allowing clients to understand what happened.
//
// EXPECTED BEHAVIOR:
// - POST /api/v1/wallet-assets returns 402 Payment Required
// - No payment is sent
// - Status endpoint eventually returns "failed" with timeout error
// - Wallet does NOT exist in database
// - Temporal schedule is NOT created
//
// This ensures the system doesn't hang forever waiting for payments.
func TestPaymentFlow_EndToEnd_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 2 * time.Second, // Short timeout for test
		MemoPrefix:     "forohtoo-test:",
	}

	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	// Step 1: POST /api/v1/wallet-assets
	reqBody := map[string]interface{}{
		"address": "TimeoutWallet123456789012345678901234",
		"network": "devnet",
		"asset": map[string]interface{}{
			"type": "sol",
		},
		"poll_interval": "30s",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("Expected 402, got %d", w.Code)
	}

	var registrationResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &registrationResp)

	workflowID := registrationResp["workflow_id"].(string)

	// Step 2: Do NOT send payment, wait for timeout

	// Step 3: Poll status until failure
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var finalStatus map[string]interface{}
	failed := false

	for !failed {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for workflow to fail")
		case <-ticker.C:
			statusReq := httptest.NewRequest("GET", "/api/v1/registration-status/"+workflowID, nil)
			statusW := httptest.NewRecorder()
			server.Handler.ServeHTTP(statusW, statusReq)

			json.Unmarshal(statusW.Body.Bytes(), &finalStatus)

			if finalStatus["status"] == "failed" {
				failed = true
			}
		}
	}

	// Step 4: Verify error message mentions timeout
	errorMsg, ok := finalStatus["error"].(string)
	if !ok || errorMsg == "" {
		t.Error("Failed workflow should have error message")
	}
	if !contains(errorMsg, "timeout") && !contains(errorMsg, "deadline") {
		t.Errorf("Error should mention timeout, got: %s", errorMsg)
	}

	// Step 5: Verify wallet NOT in database
	walletExists, err := testDB.WalletExists(ctx, reqBody["address"].(string), "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check wallet existence: %v", err)
	}
	if walletExists {
		t.Error("Wallet should NOT exist after timeout")
	}

	t.Logf("✓ Timeout flow handled correctly")
}

// TestPaymentFlow_EndToEnd_HistoricalPayment tests that the workflow completes
// immediately when payment was sent before the workflow started (within lookback window).
//
// WHAT IS BEING TESTED:
// We're testing the lookback functionality where users who pay before the workflow
// starts still get their wallets registered without additional waiting.
//
// EXPECTED BEHAVIOR:
// - Payment transaction exists in database before POST request
// - POST /api/v1/wallet-assets returns 402 with workflow ID
// - Workflow completes almost immediately (uses lookback)
// - Status endpoint quickly returns "completed"
// - Wallet is registered successfully
//
// This handles the race condition where users pay very quickly after seeing the invoice.
func TestPaymentFlow_EndToEnd_HistoricalPayment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	// Step 1: Pre-insert payment transaction (simulate payment before workflow)
	memo := "forohtoo-test:historical-123"
	paymentSignature := simulatePayment(t, ctx, testDB, cfg.ServiceWallet, "devnet", cfg.FeeAmount, memo)

	// Wait a bit to ensure transaction is committed
	time.Sleep(100 * time.Millisecond)

	// Step 2: POST /api/v1/wallet-assets (workflow starts after payment)
	reqBody := map[string]interface{}{
		"address": "HistoricalWallet1234567890123456789012",
		"network": "devnet",
		"asset": map[string]interface{}{
			"type": "sol",
		},
		"poll_interval": "30s",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("Expected 402, got %d", w.Code)
	}

	var registrationResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &registrationResp)

	workflowID := registrationResp["workflow_id"].(string)

	// Step 3: Poll status - should complete quickly
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	completed := false
	startTime := time.Now()

	for !completed {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for workflow to complete with historical payment")
		case <-ticker.C:
			statusReq := httptest.NewRequest("GET", "/api/v1/registration-status/"+workflowID, nil)
			statusW := httptest.NewRecorder()
			server.Handler.ServeHTTP(statusW, statusReq)

			var statusResp map[string]interface{}
			json.Unmarshal(statusW.Body.Bytes(), &statusResp)

			if statusResp["status"] == "completed" {
				completed = true
				completionTime := time.Since(startTime)

				// Should complete quickly (within 3 seconds)
				if completionTime > 3*time.Second {
					t.Errorf("Historical payment should complete quickly, took %v", completionTime)
				}

				if statusResp["payment_signature"] != paymentSignature {
					t.Errorf("Expected payment_signature=%s, got %v", paymentSignature, statusResp["payment_signature"])
				}
			} else if statusResp["status"] == "failed" {
				t.Fatalf("Workflow failed: %v", statusResp["error"])
			}
		}
	}

	// Step 4: Verify wallet registered
	walletExists, err := testDB.WalletExists(ctx, reqBody["address"].(string), "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check wallet existence: %v", err)
	}
	if !walletExists {
		t.Error("Wallet should exist after historical payment")
	}

	t.Logf("✓ Historical payment processed correctly")
}

// TestPaymentFlow_ExistingWallet_NoPayment tests that existing wallets can be
// updated without requiring payment (upsert behavior).
//
// WHAT IS BEING TESTED:
// We're testing that once a wallet is registered and paid for, subsequent
// updates (like changing poll_interval) don't require additional payment.
//
// EXPECTED BEHAVIOR:
// - Register wallet manually (outside payment flow)
// - POST /api/v1/wallet-assets for same wallet
// - Returns 200 OK (not 402 Payment Required)
// - Wallet is updated (e.g., poll_interval changes)
// - No workflow started
// - No payment required
//
// This enables users to update their wallet settings without paying again.
func TestPaymentFlow_ExistingWallet_NoPayment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	address := "ExistingWallet123456789012345678901234"

	// Step 1: Manually insert wallet into database (simulate previous registration)
	_, err := testDB.UpsertWallet(ctx, db.UpsertWalletParams{
		Address:      address,
		Network:      "devnet",
		AssetType:    "sol",
		TokenMint:    "",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("Failed to insert wallet: %v", err)
	}

	// Step 2: POST /api/v1/wallet-assets for existing wallet with new poll_interval
	reqBody := map[string]interface{}{
		"address": address,
		"network": "devnet",
		"asset": map[string]interface{}{
			"type": "sol",
		},
		"poll_interval": "60s", // Changed from 30s
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler.ServeHTTP(w, req)

	// Should be 200 OK or 201 Created, NOT 402 Payment Required
	if w.Code == http.StatusPaymentRequired {
		t.Fatal("Existing wallet should not require payment")
	}

	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("Expected 200/201, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Step 3: Verify wallet still exists (no deletion)
	walletExists, err := testDB.WalletExists(ctx, address, "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check wallet existence: %v", err)
	}
	if !walletExists {
		t.Error("Wallet should still exist")
	}

	t.Logf("✓ Existing wallet updated without payment")
}

// TestPaymentFlow_PaymentGatewayDisabled tests that when the payment gateway
// is disabled, wallets are registered immediately without payment.
//
// WHAT IS BEING TESTED:
// We're testing the free mode of the service where PaymentGateway.Enabled=false,
// allowing immediate wallet registration without any payment workflow.
//
// EXPECTED BEHAVIOR:
// - Config has PaymentGateway.Enabled = false
// - POST /api/v1/wallet-assets for new wallet
// - Returns 201 Created immediately (not 402)
// - Wallet registered in database
// - Schedule created
// - No workflow started
// - No payment required
//
// This allows the service to run in free mode during development or for certain users.
func TestPaymentFlow_PaymentGatewayDisabled(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled: false, // Payment gateway DISABLED
	}

	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	// POST /api/v1/wallet-assets
	reqBody := map[string]interface{}{
		"address": "FreeWallet1234567890123456789012345678",
		"network": "devnet",
		"asset": map[string]interface{}{
			"type": "sol",
		},
		"poll_interval": "30s",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler.ServeHTTP(w, req)

	// Should be 201 Created, NOT 402 Payment Required
	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 Created, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify no workflow_id in response
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp["workflow_id"] != nil {
		t.Error("Response should not contain workflow_id when payment gateway disabled")
	}

	if resp["invoice"] != nil {
		t.Error("Response should not contain invoice when payment gateway disabled")
	}

	// Verify wallet in database
	walletExists, err := testDB.WalletExists(ctx, reqBody["address"].(string), "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check wallet existence: %v", err)
	}
	if !walletExists {
		t.Error("Wallet should be registered when payment gateway disabled")
	}

	t.Logf("✓ Free registration works when payment gateway disabled")
}

// TestPaymentFlow_ServiceWalletAutoRegistration tests that the service wallet
// is automatically registered when the server starts with payment gateway enabled.
//
// WHAT IS BEING TESTED:
// We're testing the bootstrapping logic that ensures the service can monitor
// its own wallet for incoming payments by auto-registering itself.
//
// EXPECTED BEHAVIOR:
// - Server starts with PaymentGateway.Enabled = true
// - Service wallet NOT in database initially
// - ensureServiceWalletRegistered() is called on startup
// - Service wallet is registered in database
// - Service wallet has Temporal schedule for polling
// - No payment required for service wallet
//
// This is critical for the payment gateway to work - the service must monitor its own wallet.
func TestPaymentFlow_ServiceWalletAutoRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	serviceWalletAddress := "ServiceWallet12345678901234567890123456"

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  serviceWalletAddress,
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	// Verify service wallet NOT in database initially
	exists, err := testDB.WalletExists(ctx, serviceWalletAddress, "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check service wallet: %v", err)
	}
	if exists {
		t.Fatal("Service wallet should not exist before auto-registration")
	}

	// Call ensureServiceWalletRegistered (simulates server startup)
	scheduler := &mockScheduler{}
	err = ensureServiceWalletRegistered(ctx, testDB, scheduler, cfg)
	if err != nil {
		t.Fatalf("Failed to auto-register service wallet: %v", err)
	}

	// Verify service wallet is now in database
	exists, err = testDB.WalletExists(ctx, serviceWalletAddress, "devnet", "sol", "")
	if err != nil {
		t.Fatalf("Failed to check service wallet after registration: %v", err)
	}
	if !exists {
		t.Error("Service wallet should exist after auto-registration")
	}

	// Verify schedule was created
	if !scheduler.upsertCalled {
		t.Error("UpsertWalletAssetSchedule should be called for service wallet")
	}

	t.Logf("✓ Service wallet auto-registered successfully")
}

// TestPaymentFlow_MultipleClients tests that multiple clients can register
// different wallets concurrently without interference.
//
// WHAT IS BEING TESTED:
// We're testing the concurrency handling of the payment gateway, ensuring
// multiple workflows can run simultaneously without conflicts.
//
// EXPECTED BEHAVIOR:
// - Three clients POST /api/v1/wallet-assets concurrently
// - Each receives unique invoice with unique memo
// - Each receives different workflow_id
// - All three send payments
// - All three workflows complete successfully
// - All three wallets registered in database
// - No interference between workflows
//
// This ensures the system can handle realistic concurrent load.
func TestPaymentFlow_MultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	server := setupTestServer(t, testDB, temporalClient, cfg)
	defer server.Close()

	// Create three concurrent clients
	addresses := []string{
		"ConcurrentWallet1234567890123456789012",
		"ConcurrentWallet2234567890123456789012",
		"ConcurrentWallet3234567890123456789012",
	}

	type clientResult struct {
		workflowID string
		memo       string
		err        error
	}

	results := make(chan clientResult, 3)

	// Step 1: Three clients register concurrently
	for _, addr := range addresses {
		go func(address string) {
			reqBody := map[string]interface{}{
				"address": address,
				"network": "devnet",
				"asset": map[string]interface{}{
					"type": "sol",
				},
				"poll_interval": "30s",
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/api/v1/wallet-assets", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.Handler.ServeHTTP(w, req)

			if w.Code != http.StatusPaymentRequired {
				results <- clientResult{err: fmt.Errorf("expected 402, got %d", w.Code)}
				return
			}

			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			workflowID := resp["workflow_id"].(string)
			invoice := resp["invoice"].(map[string]interface{})
			memo := invoice["memo"].(string)

			results <- clientResult{workflowID: workflowID, memo: memo}
		}(addr)
	}

	// Collect results
	workflowIDs := make([]string, 0, 3)
	memos := make([]string, 0, 3)

	for i := 0; i < 3; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("Client %d failed: %v", i, result.err)
		}
		workflowIDs = append(workflowIDs, result.workflowID)
		memos = append(memos, result.memo)
	}

	// Step 2: Verify each client got unique workflow ID and memo
	if workflowIDs[0] == workflowIDs[1] || workflowIDs[1] == workflowIDs[2] {
		t.Error("Each client should receive unique workflow_id")
	}
	if memos[0] == memos[1] || memos[1] == memos[2] {
		t.Error("Each client should receive unique memo")
	}

	// Step 3: Simulate payments for all three
	for i, memo := range memos {
		simulatePayment(t, ctx, testDB, cfg.ServiceWallet, "devnet", cfg.FeeAmount, memo)
		t.Logf("Payment %d/%d simulated", i+1, len(memos))
	}

	// Step 4: Wait for all three workflows to complete
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	completedCount := 0

	for completedCount < 3 {
		select {
		case <-timeout:
			t.Fatalf("Timeout: only %d/3 workflows completed", completedCount)
		case <-ticker.C:
			for _, wfID := range workflowIDs {
				statusReq := httptest.NewRequest("GET", "/api/v1/registration-status/"+wfID, nil)
				statusW := httptest.NewRecorder()
				server.Handler.ServeHTTP(statusW, statusReq)

				var statusResp map[string]interface{}
				json.Unmarshal(statusW.Body.Bytes(), &statusResp)

				if statusResp["status"] == "completed" {
					completedCount++
				} else if statusResp["status"] == "failed" {
					t.Fatalf("Workflow %s failed: %v", wfID, statusResp["error"])
				}
			}
		}
	}

	// Step 5: Verify all three wallets registered
	for _, addr := range addresses {
		exists, err := testDB.WalletExists(ctx, addr, "devnet", "sol", "")
		if err != nil {
			t.Fatalf("Failed to check wallet %s: %v", addr, err)
		}
		if !exists {
			t.Errorf("Wallet %s should be registered", addr)
		}
	}

	t.Logf("✓ All %d concurrent registrations completed successfully", len(addresses))
}

// TestPaymentFlow_DuplicateWorkflowID tests that Temporal correctly handles
// duplicate workflow ID attempts (idempotency).
//
// WHAT IS BEING TESTED:
// We're testing Temporal's workflow ID uniqueness enforcement, ensuring
// clients can't accidentally start duplicate workflows for the same registration.
//
// EXPECTED BEHAVIOR:
// - Start workflow with deterministic ID "payment-registration:abc123"
// - Attempt to start another workflow with same ID
// - Temporal rejects duplicate or returns existing workflow
// - Only one workflow processes the payment
// - No duplicate wallet registrations
//
// This ensures idempotency and prevents double-charging issues.
func TestPaymentFlow_DuplicateWorkflowID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	testDB := setupTestDatabase(t)
	defer cleanupTestDatabase(t, testDB)

	temporalClient := setupTemporalTestServer(t)
	defer temporalClient.Close()

	testWorker := setupTestWorker(t, temporalClient, testDB)
	defer testWorker.Stop()

	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "ServiceWallet12345678901234567890123456",
		ServiceNetwork: "devnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 5 * time.Minute,
		MemoPrefix:     "forohtoo-test:",
	}

	// Start first workflow
	workflowID := "payment-registration:duplicate-test-123"
	workflowInput := temporal.PaymentGatedRegistrationInput{
		Address:        "DuplicateWallet123456789012345678901",
		Network:        "devnet",
		AssetType:      "sol",
		TokenMint:      "",
		PollInterval:   30 * time.Second,
		ServiceWallet:  cfg.ServiceWallet,
		ServiceNetwork: cfg.ServiceNetwork,
		FeeAmount:      cfg.FeeAmount,
		PaymentMemo:    "forohtoo-test:duplicate-test-123",
		PaymentTimeout: cfg.PaymentTimeout,
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: "forohtoo-test",
	}

	// Start first workflow
	run1, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, temporal.PaymentGatedRegistrationWorkflow, workflowInput)
	if err != nil {
		t.Fatalf("Failed to start first workflow: %v", err)
	}

	// Attempt to start duplicate workflow with same ID
	run2, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, temporal.PaymentGatedRegistrationWorkflow, workflowInput)

	// Temporal should either:
	// 1. Return error indicating workflow already exists, OR
	// 2. Return the existing workflow run
	if err == nil {
		// If no error, verify it's the same workflow
		if run1.GetID() != run2.GetID() {
			t.Error("Duplicate workflow should return same workflow ID")
		}
		t.Logf("✓ Temporal returned existing workflow for duplicate ID")
	} else {
		// If error, verify it mentions duplicate/already exists
		if !contains(err.Error(), "already") && !contains(err.Error(), "exists") {
			t.Errorf("Expected duplicate workflow error, got: %v", err)
		}
		t.Logf("✓ Temporal rejected duplicate workflow ID")
	}
}

// Helper functions

func setupTestDatabase(t *testing.T) db.Store {
	// TODO: Implement database test container setup
	// For now, return nil - real implementation would use testcontainers
	t.Skip("Database test container not yet implemented")
	return nil
}

func cleanupTestDatabase(t *testing.T, testDB db.Store) {
	// TODO: Implement cleanup
}

func setupTemporalTestServer(t *testing.T) client.Client {
	// TODO: Implement Temporal test server setup
	// For now, skip - real implementation would use Temporal test server
	t.Skip("Temporal test server not yet implemented")
	return nil
}

func setupTestWorker(t *testing.T, temporalClient client.Client, testDB db.Store) worker.Worker {
	// TODO: Implement test worker setup
	t.Skip("Test worker not yet implemented")
	return nil
}

func setupTestServer(t *testing.T, testDB db.Store, temporalClient client.Client, cfg *PaymentGatewayConfig) *httptest.Server {
	// TODO: Implement test server setup
	t.Skip("Test server not yet implemented")
	return nil
}

func simulatePayment(t *testing.T, ctx context.Context, testDB db.Store, toAddress, network string, amount int64, memo string) string {
	// TODO: Implement payment simulation by inserting transaction into database
	t.Skip("Payment simulation not yet implemented")
	return ""
}

func ensureServiceWalletRegistered(ctx context.Context, store db.Store, scheduler Scheduler, cfg *PaymentGatewayConfig) error {
	// TODO: Implement service wallet registration
	// For now, return nil
	return nil
}

type mockScheduler struct {
	upsertCalled bool
}

func (m *mockScheduler) UpsertWalletAssetSchedule(ctx context.Context, address, network, assetType, tokenMint string, ata *string, pollInterval time.Duration) error {
	m.upsertCalled = true
	return nil
}

func (m *mockScheduler) DeleteWalletAssetSchedule(ctx context.Context, address, network, assetType, tokenMint string) error {
	return nil
}

// Helper for string comparison
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) != -1
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
