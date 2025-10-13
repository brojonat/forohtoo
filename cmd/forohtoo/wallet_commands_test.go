package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/itchyny/gojq"
	"github.com/urfave/cli/v2"
)

func TestJQFilterMatching(t *testing.T) {
	tests := []struct {
		name          string
		memo          string
		jqFilter      string
		expectMatch   bool
		expectErr     bool
	}{
		{
			name:        "simple workflow_id match",
			memo:        `{"workflow_id": "test-123"}`,
			jqFilter:    `. | contains({workflow_id: "test-123"})`,
			expectMatch: true,
		},
		{
			name:        "workflow_id mismatch",
			memo:        `{"workflow_id": "other-456"}`,
			jqFilter:    `. | contains({workflow_id: "test-123"})`,
			expectMatch: false,
		},
		{
			name:        "nested object match",
			memo:        `{"metadata": {"order_id": "12345"}, "workflow_id": "test"}`,
			jqFilter:    `.metadata.order_id == "12345"`,
			expectMatch: true,
		},
		{
			name:        "nested object mismatch",
			memo:        `{"metadata": {"order_id": "67890"}, "workflow_id": "test"}`,
			jqFilter:    `.metadata.order_id == "12345"`,
			expectMatch: false,
		},
		{
			name:        "invalid JSON memo",
			memo:        `not-json`,
			jqFilter:    `. | contains({workflow_id: "test"})`,
			expectMatch: false,
			expectErr:   true,
		},
		{
			name:        "true boolean result",
			memo:        `{"amount": 100}`,
			jqFilter:    `.amount > 50`,
			expectMatch: true,
		},
		{
			name:        "false boolean result",
			memo:        `{"amount": 25}`,
			jqFilter:    `.amount > 50`,
			expectMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse jq filter
			query, err := gojq.Parse(tt.jqFilter)
			if err != nil {
				t.Fatalf("failed to parse jq filter: %v", err)
			}
			code, err := gojq.Compile(query)
			if err != nil {
				t.Fatalf("failed to compile jq filter: %v", err)
			}

			// Parse memo as JSON
			var memoJSON interface{}
			err = json.Unmarshal([]byte(tt.memo), &memoJSON)
			if err != nil && !tt.expectErr {
				t.Fatalf("unexpected JSON parse error: %v", err)
			}
			if err != nil && tt.expectErr {
				// Expected error, test passes
				return
			}

			// Run jq filter
			iter := code.Run(memoJSON)
			v, ok := iter.Next()
			if !ok {
				if tt.expectMatch {
					t.Fatal("expected match but jq filter returned no result")
				}
				return
			}

			if err, isErr := v.(error); isErr {
				if !tt.expectErr {
					t.Fatalf("unexpected jq filter error: %v", err)
				}
				return
			}

			// Check truthiness
			matched := isTruthy(v)
			if matched != tt.expectMatch {
				t.Errorf("expected match=%v, got match=%v (jq result: %v)", tt.expectMatch, matched, v)
			}
		})
	}
}

func TestUSDCAmountCalculation(t *testing.T) {
	tests := []struct {
		name           string
		usdcAmount     float64
		txnAmount      int64
		expectMatch    bool
	}{
		{
			name:        "exact match 0.42 USDC",
			usdcAmount:  0.42,
			txnAmount:   420000, // 0.42 * 1e6
			expectMatch: true,
		},
		{
			name:        "exact match 1.0 USDC",
			usdcAmount:  1.0,
			txnAmount:   1000000, // 1.0 * 1e6
			expectMatch: true,
		},
		{
			name:        "mismatch - too high",
			usdcAmount:  0.42,
			txnAmount:   500000,
			expectMatch: false,
		},
		{
			name:        "mismatch - too low",
			usdcAmount:  0.42,
			txnAmount:   400000,
			expectMatch: false,
		},
		{
			name:        "small amount 0.000001 USDC",
			usdcAmount:  0.000001,
			txnAmount:   1, // smallest USDC unit
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectedLamports := int64(tt.usdcAmount * 1e6)
			matched := tt.txnAmount == expectedLamports

			if matched != tt.expectMatch {
				t.Errorf("expected match=%v, got match=%v (expected lamports: %d, actual: %d)",
					tt.expectMatch, matched, expectedLamports, tt.txnAmount)
			}
		})
	}
}

func TestMatcherClosure(t *testing.T) {
	// Test that multiple conditions work together (AND logic)
	txn := &client.Transaction{
		Signature:     "test-sig",
		Amount:        420000, // 0.42 USDC
		Memo:          `{"workflow_id": "test-123", "amount_usd": 0.42}`,
		WalletAddress: "test-wallet",
	}

	// Parse jq filter
	query, err := gojq.Parse(`. | contains({workflow_id: "test-123"})`)
	if err != nil {
		t.Fatalf("failed to parse jq filter: %v", err)
	}
	code, err := gojq.Compile(query)
	if err != nil {
		t.Fatalf("failed to compile jq filter: %v", err)
	}

	// Build matcher with multiple conditions (similar to CLI implementation)
	workflowID := "test-123"
	usdcAmount := 0.42
	matcher := func(txn *client.Transaction) bool {
		// Check workflow_id
		if workflowID != "" && txn.Memo != "" {
			var memoJSON interface{}
			if unmarshalErr := json.Unmarshal([]byte(txn.Memo), &memoJSON); unmarshalErr != nil {
				return false
			}
			iter := code.Run(memoJSON)
			v, ok := iter.Next()
			if !ok {
				return false
			}
			if filterErr, isErr := v.(error); isErr {
				_ = filterErr // Silence unused warning
				return false
			}
			if !isTruthy(v) {
				return false
			}
		}

		// Check USDC amount
		if usdcAmount != 0 {
			expectedLamports := int64(usdcAmount * 1e6)
			if txn.Amount != expectedLamports {
				return false
			}
		}

		return true
	}

	// Test matching transaction
	if !matcher(txn) {
		t.Error("expected transaction to match all conditions")
	}

	// Test with wrong amount
	txnWrongAmount := *txn
	txnWrongAmount.Amount = 100000
	if matcher(&txnWrongAmount) {
		t.Error("expected transaction with wrong amount to not match")
	}

	// Test with wrong workflow_id
	txnWrongWorkflow := *txn
	txnWrongWorkflow.Memo = `{"workflow_id": "wrong-id", "amount_usd": 0.42}`
	if matcher(&txnWrongWorkflow) {
		t.Error("expected transaction with wrong workflow_id to not match")
	}
}

// Test helpers for mocking HTTP server

func TestWalletAddCommand(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/wallets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Parse request body
		var req struct {
			Address      string `json:"address"`
			PollInterval string `json:"poll_interval"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Validate request
		if req.Address != "test-wallet-123" {
			t.Errorf("unexpected address: %s", req.Address)
		}
		if req.PollInterval != "30s" {
			t.Errorf("unexpected poll_interval: %s", req.PollInterval)
		}

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "add", "--server", server.URL, "test-wallet-123"})
	
	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("✓ Wallet registered successfully")) {
		t.Errorf("expected success message, got: %s", output)
	}
}

func TestWalletAddCommand_JSON(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "add", "--server", server.URL, "--json", "test-wallet"})
	
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify JSON output
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected JSON output, got: %s", output)
	}

	if result["address"] != "test-wallet" {
		t.Errorf("expected address=test-wallet, got: %v", result["address"])
	}
}

func TestWalletListCommand(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	now := time.Now()
	lastPoll := now.Add(-5 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/wallets" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		wallets := struct {
			Wallets []map[string]interface{} `json:"wallets"`
		}{
			Wallets: []map[string]interface{}{
				{
					"address":       "wallet1",
					"poll_interval": "30s",
					"last_poll_time": lastPoll.Format(time.RFC3339),
					"status":        "active",
					"created_at":    now.Format(time.RFC3339),
					"updated_at":    now.Format(time.RFC3339),
				},
				{
					"address":       "wallet2",
					"poll_interval": "1m",
					"last_poll_time": nil,
					"status":        "active",
					"created_at":    now.Format(time.RFC3339),
					"updated_at":    now.Format(time.RFC3339),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wallets)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "list", "--server", server.URL})

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should output JSON array by default
	var wallets []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &wallets); err != nil {
		t.Fatalf("expected JSON array output, got: %s", output)
	}

	if len(wallets) != 2 {
		t.Errorf("expected 2 wallets, got: %d", len(wallets))
	}

	// Verify wallet structure
	if wallets[0]["address"] != "wallet1" {
		t.Errorf("expected first wallet address to be wallet1, got: %v", wallets[0]["address"])
	}
}

func TestWalletListCommand_Empty(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wallets := struct {
			Wallets []map[string]interface{} `json:"wallets"`
		}{
			Wallets: []map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wallets)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "list", "--server", server.URL})
	
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Should output JSON array by default, even for empty list
	var wallets []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &wallets); err != nil {
		t.Fatalf("expected JSON array output, got: %s", output)
	}

	if len(wallets) != 0 {
		t.Errorf("expected 0 wallets, got: %d", len(wallets))
	}
}

func TestWalletGetCommand(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	now := time.Now()
	lastPoll := now.Add(-10 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/v1/wallets/test-wallet" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		wallet := map[string]interface{}{
			"address":       "test-wallet",
			"poll_interval": "45s",
			"last_poll_time": lastPoll.Format(time.RFC3339),
			"status":        "active",
			"created_at":    now.Format(time.RFC3339),
			"updated_at":    now.Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wallet)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "get", "--server", server.URL, "test-wallet"})
	
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("test-wallet")) {
		t.Errorf("expected test-wallet in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("45s")) {
		t.Errorf("expected 45s poll interval in output, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("active")) {
		t.Errorf("expected active status in output, got: %s", output)
	}
}

func TestWalletGetCommand_NotFound(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "wallet not found",
		})
	}))
	defer server.Close()

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "get", "--server", server.URL, "nonexistent"})
	
	if err == nil {
		t.Fatal("expected error for nonexistent wallet")
	}

	if !bytes.Contains([]byte(err.Error()), []byte("wallet not found")) {
		t.Errorf("expected 'wallet not found' error, got: %v", err)
	}
}

func TestWalletRemoveCommand(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got: %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/v1/wallets/test-wallet" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	app := &cli.App{
		Commands: []*cli.Command{
			walletCommands(),
		},
	}

	err := app.Run([]string{"test", "wallet", "remove", "--server", server.URL, "test-wallet"})
	
	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("✓ Wallet unregistered successfully")) {
		t.Errorf("expected success message, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("test-wallet")) {
		t.Errorf("expected test-wallet in output, got: %s", output)
	}
}

func TestWalletRemoveCommand_Aliases(t *testing.T) {
	// Unset environment variables that might interfere
	os.Unsetenv("FOROHTOO_SERVER_URL")
	os.Unsetenv("SERVER_URL")

	aliases := []string{"remove", "rm", "delete", "unregister"}

	for _, alias := range aliases {
		t.Run(alias, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "DELETE" {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			// Suppress output
			oldStdout := os.Stdout
			os.Stdout, _ = os.Open(os.DevNull)
			defer func() { os.Stdout = oldStdout }()

			app := &cli.App{
				Commands: []*cli.Command{
					walletCommands(),
				},
			}

			err := app.Run([]string{"test", "wallet", alias, "--server", server.URL, "test-wallet"})
			
			if err != nil {
				t.Errorf("alias %s failed: %v", alias, err)
			}
		})
	}
}
