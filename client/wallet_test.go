package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/wallet-assets", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)

		assert.Equal(t, "wallet123", body["address"])
		assert.Equal(t, "mainnet", body["network"])

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.RegisterAsset(context.Background(), "wallet123", "mainnet", "sol", "")
	assert.NoError(t, err)
}

func TestRegister_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "invalid wallet address",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.RegisterAsset(context.Background(), "invalid", "mainnet", "sol", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid wallet address")
}

func TestUnregister_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/wallet-assets/wallet123", r.URL.Path)
		assert.Equal(t, "mainnet", r.URL.Query().Get("network"))

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.UnregisterAsset(context.Background(), "wallet123", "mainnet", "sol", "")
	assert.NoError(t, err)
}

func TestUnregister_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "wallet not found",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.UnregisterAsset(context.Background(), "nonexistent", "mainnet", "sol", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wallet not found")
}

func TestGet_Success(t *testing.T) {
	now := time.Now()
	lastPoll := now.Add(-5 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/wallet-assets/wallet123", r.URL.Path)
		assert.Equal(t, "mainnet", r.URL.Query().Get("network"))

		// Return response in server format
		response := map[string]interface{}{
			"address":        "wallet123",
			"network":        "mainnet",
			"asset_type":     "sol",
			"token_mint":     "",
			"poll_interval":  "30s",
			"last_poll_time": lastPoll,
			"status":         "active",
			"created_at":     now.Add(-1 * time.Hour),
			"updated_at":     now,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallet, err := client.Get(context.Background(), "wallet123", "mainnet")
	require.NoError(t, err)
	require.NotNil(t, wallet)

	assert.Equal(t, "wallet123", wallet.Address)
	assert.Equal(t, "mainnet", wallet.Network)
	assert.Equal(t, "active", wallet.Status)
	assert.Equal(t, 30*time.Second, wallet.PollInterval)
	assert.NotNil(t, wallet.LastPollTime)
}

func TestGet_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "wallet not found",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallet, err := client.Get(context.Background(), "nonexistent", "mainnet")
	require.Error(t, err)
	assert.Nil(t, wallet)
	assert.Contains(t, err.Error(), "wallet not found")
}

func TestList_Success(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/wallet-assets", r.URL.Path)

		// Return response in server format
		response := map[string]interface{}{
			"wallets": []map[string]interface{}{
				{
					"address":       "wallet123",
					"network":       "mainnet",
					"asset_type":    "sol",
					"token_mint":    "",
					"poll_interval": "30s",
					"status":        "active",
					"created_at":    now,
					"updated_at":    now,
				},
				{
					"address":       "wallet456",
					"network":       "mainnet",
					"asset_type":    "sol",
					"token_mint":    "",
					"poll_interval": "30s",
					"status":        "active",
					"created_at":    now,
					"updated_at":    now,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallets, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, wallets, 2)

	assert.Equal(t, "wallet123", wallets[0].Address)
	assert.Equal(t, "wallet456", wallets[1].Address)
}

func TestList_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := struct {
			Wallets []*Wallet `json:"wallets"`
		}{
			Wallets: []*Wallet{},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallets, err := client.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, wallets)
}

func TestList_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "database connection failed",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallets, err := client.List(context.Background())
	require.Error(t, err)
	assert.Nil(t, wallets)
	assert.Contains(t, err.Error(), "database connection failed")
}

// TestClient_Await_MatchingTransaction tests that client.Await() returns
// immediately when a matching transaction is received via SSE.
//
// WHAT IS BEING TESTED:
// We're testing the core payment detection functionality used by the payment
// gateway - the ability to wait for and match specific transactions via SSE.
//
// EXPECTED BEHAVIOR:
// - SSE stream sends transactions
// - Matcher finds transaction with correct amount and memo
// - client.Await() returns immediately
// - Returned transaction has correct signature, amount, memo
// - No timeout error
//
// This is the happy path for payment detection.
func TestClient_Await_MatchingTransaction(t *testing.T) {
	// Mock SSE server that sends matching transaction
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/v1/transactions")

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "ResponseWriter should support flushing")

		// Send matching transaction
		transaction := Transaction{
			Signature: "matching-sig-123",
			BlockTime: time.Now(),
			Amount:    1000000,
			Memo:      stringPtr("forohtoo-reg:abc123"),
		}

		data, _ := json.Marshal(transaction)
		_, err := w.Write([]byte("data: " + string(data) + "\n\n"))
		require.NoError(t, err)
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)

	// Matcher that checks amount and memo
	matcher := func(tx *Transaction) bool {
		return tx.Amount == 1000000 && tx.Memo != nil && *tx.Memo == "forohtoo-reg:abc123"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := client.Await(ctx, "wallet123", "mainnet", 1*time.Hour, matcher)
	require.NoError(t, err)
	require.NotNil(t, tx)

	assert.Equal(t, "matching-sig-123", tx.Signature)
	assert.Equal(t, int64(1000000), tx.Amount)
	assert.NotNil(t, tx.Memo)
	assert.Equal(t, "forohtoo-reg:abc123", *tx.Memo)

	t.Logf("✓ Await found matching transaction")
}

// TestClient_Await_NonMatchingTransactions tests that client.Await() continues
// waiting when transactions don't match the criteria.
//
// WHAT IS BEING TESTED:
// We're testing the matcher filtering logic - transactions that don't meet
// the criteria should be rejected and the wait should continue.
//
// EXPECTED BEHAVIOR:
// - SSE stream sends multiple transactions
// - Matcher rejects all of them (wrong amount, wrong memo, etc.)
// - client.Await() continues waiting
// - Eventually times out (or in real scenario, waits for correct transaction)
//
// This ensures the matcher correctly filters out invalid payments.
func TestClient_Await_NonMatchingTransactions(t *testing.T) {
	// Mock SSE server that sends non-matching transactions
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// Send transaction with wrong amount
		tx1 := Transaction{
			Signature: "wrong-amount-sig",
			BlockTime: time.Now(),
			Amount:    500000, // Wrong amount
			Memo:      stringPtr("forohtoo-reg:abc123"),
		}
		data1, _ := json.Marshal(tx1)
		w.Write([]byte("data: " + string(data1) + "\n\n"))
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		// Send transaction with wrong memo
		tx2 := Transaction{
			Signature: "wrong-memo-sig",
			BlockTime: time.Now(),
			Amount:    1000000,
			Memo:      stringPtr("forohtoo-reg:xyz789"), // Wrong memo
		}
		data2, _ := json.Marshal(tx2)
		w.Write([]byte("data: " + string(data2) + "\n\n"))
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		// Send transaction with no memo
		tx3 := Transaction{
			Signature: "no-memo-sig",
			BlockTime: time.Now(),
			Amount:    1000000,
			Memo:      nil, // No memo
		}
		data3, _ := json.Marshal(tx3)
		w.Write([]byte("data: " + string(data3) + "\n\n"))
		flusher.Flush()

		// Keep connection open until client times out
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)

	// Matcher that requires specific amount and memo
	matcher := func(tx *Transaction) bool {
		return tx.Amount == 1000000 && tx.Memo != nil && *tx.Memo == "forohtoo-reg:abc123"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	tx, err := client.Await(ctx, "wallet123", "mainnet", 1*time.Hour, matcher)

	// Should timeout because no matching transaction
	require.Error(t, err)
	assert.Nil(t, tx)
	assert.Equal(t, context.DeadlineExceeded, err)

	t.Logf("✓ Await correctly rejected non-matching transactions")
}

// TestClient_Await_Timeout tests that client.Await() returns a timeout error
// when no matching transaction arrives within the context deadline.
//
// WHAT IS BEING TESTED:
// We're testing the timeout behavior when users never send payment. This is
// critical for preventing workflows from hanging indefinitely.
//
// EXPECTED BEHAVIOR:
// - Context has timeout set
// - No matching transaction arrives
// - client.Await() returns context.DeadlineExceeded error
// - No panic or hang
//
// This ensures timeouts work correctly for payment workflows.
func TestClient_Await_Timeout(t *testing.T) {
	// Mock SSE server that sends nothing
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Keep connection open but send nothing
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)

	matcher := func(tx *Transaction) bool {
		return true // Accept any transaction (but none will arrive)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	tx, err := client.Await(ctx, "wallet123", "mainnet", 1*time.Hour, matcher)
	elapsed := time.Since(startTime)

	// Should timeout
	require.Error(t, err)
	assert.Nil(t, tx)
	assert.Equal(t, context.DeadlineExceeded, err)

	// Should timeout around 500ms (allow some tolerance)
	assert.Greater(t, elapsed, 400*time.Millisecond)
	assert.Less(t, elapsed, 1*time.Second)

	t.Logf("✓ Await timed out correctly after %v", elapsed)
}

// TestClient_Await_ContextCancelled tests that client.Await() returns a
// cancellation error when the context is cancelled while waiting.
//
// WHAT IS BEING TESTED:
// We're testing graceful cancellation, which is important when workflows
// are cancelled or when the server shuts down.
//
// EXPECTED BEHAVIOR:
// - Context is manually cancelled while waiting
// - client.Await() returns context.Canceled error
// - SSE connection is closed
// - No resource leaks
//
// This ensures workflows can be cancelled cleanly.
func TestClient_Await_ContextCancelled(t *testing.T) {
	// Mock SSE server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Keep connection open until cancelled
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)

	matcher := func(tx *Transaction) bool {
		return true
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after 200ms
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	startTime := time.Now()
	tx, err := client.Await(ctx, "wallet123", "mainnet", 1*time.Hour, matcher)
	elapsed := time.Since(startTime)

	// Should be cancelled
	require.Error(t, err)
	assert.Nil(t, tx)
	assert.Equal(t, context.Canceled, err)

	// Should cancel around 200ms
	assert.Greater(t, elapsed, 150*time.Millisecond)
	assert.Less(t, elapsed, 500*time.Millisecond)

	t.Logf("✓ Await cancelled correctly after %v", elapsed)
}

// TestClient_Await_LookbackFindsTransaction tests that client.Await() uses
// the lookback period to find historical transactions.
//
// WHAT IS BEING TESTED:
// We're testing the lookback functionality where transactions that occurred
// before the Await() call can still be detected if they're within the lookback window.
//
// EXPECTED BEHAVIOR:
// - Lookback period: 24h
// - Matching transaction exists 12h ago (within lookback)
// - client.Await() returns immediately with historical transaction
// - No waiting for new transactions
//
// This handles the race condition where users pay before workflow starts.
func TestClient_Await_LookbackFindsTransaction(t *testing.T) {
	// Mock SSE server that includes lookback query parameter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)

		// Verify lookback parameter is present
		lookbackStr := r.URL.Query().Get("lookback")
		assert.NotEmpty(t, lookbackStr, "lookback parameter should be set")

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// Send historical transaction (from 12h ago, within 24h lookback)
		historicalTx := Transaction{
			Signature: "historical-payment-sig",
			BlockTime: time.Now().Add(-12 * time.Hour),
			Amount:    1000000,
			Memo:      stringPtr("forohtoo-reg:historical-123"),
		}

		data, _ := json.Marshal(historicalTx)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)

	matcher := func(tx *Transaction) bool {
		return tx.Amount == 1000000 && tx.Memo != nil && *tx.Memo == "forohtoo-reg:historical-123"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startTime := time.Now()
	tx, err := client.Await(ctx, "wallet123", "mainnet", 24*time.Hour, matcher)
	elapsed := time.Since(startTime)

	require.NoError(t, err)
	require.NotNil(t, tx)

	assert.Equal(t, "historical-payment-sig", tx.Signature)
	assert.Equal(t, int64(1000000), tx.Amount)

	// Should find historical transaction quickly (within 2 seconds)
	assert.Less(t, elapsed, 2*time.Second)

	t.Logf("✓ Await found historical transaction via lookback in %v", elapsed)
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
