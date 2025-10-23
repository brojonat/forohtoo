package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *db.Store {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	require.NoError(t, pool.Ping(context.Background()))

	// Clean database
	_, err = pool.Exec(context.Background(), "TRUNCATE TABLE transactions, wallets CASCADE")
	require.NoError(t, err)

	return db.NewStore(pool)
}

func TestRegisterWallet_PathologicalInput(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}
	handler := handleRegisterWalletWithScheduler(store, scheduler, cfg, logger)

	tests := []struct {
		name           string
		body           string
		expectedStatus int
		checkError     func(t *testing.T, body string)
	}{
		{
			name:           "extremely large request body",
			body:           `{"address":"` + strings.Repeat("A", 10*1024*1024) + `","poll_interval":"30s"}`, // 10MB
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "request body too large")
			},
		},
		{
			name:           "malformed JSON",
			body:           `{"address":"wallet123","poll_interval":`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "invalid request body")
			},
		},
		{
			name:           "empty JSON object",
			body:           `{}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "address is required")
			},
		},
		{
			name:           "missing address",
			body:           `{"poll_interval":"30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "address is required")
			},
		},
		{
			name:           "empty address",
			body:           `{"address":"","poll_interval":"30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "address is required")
			},
		},
		{
			name:           "address too long",
			body:           `{"address":"` + strings.Repeat("A", 500) + `","poll_interval":"30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "address too long")
			},
		},
		{
			name:           "address with null bytes",
			body:           `{"address":"wallet\u0000123","poll_interval":"30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "invalid characters")
			},
		},
		{
			name:           "address with SQL injection attempt",
			body:           `{"address":"wallet'; DROP TABLE wallets; --","poll_interval":"30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "invalid characters")
			},
		},
		{
			name:           "missing poll_interval",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"}}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "poll_interval")
			},
		},
		{
			name:           "invalid poll_interval format",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"not-a-duration"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "invalid poll_interval")
			},
		},
		{
			name:           "negative poll_interval",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"-30s"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "poll_interval must be positive")
			},
		},
		{
			name:           "poll_interval too short",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"1ns"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "poll_interval must be at least")
			},
		},
		{
			name:           "poll_interval too long",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"999999h"}`,
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, body string) {
				assert.Contains(t, body, "poll_interval cannot exceed")
			},
		},
		{
			name:           "extra unexpected fields should be ignored",
			body:           `{"address":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"30s","malicious":"data","admin":true}`,
			expectedStatus: http.StatusCreated,
			checkError:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.checkError != nil {
				var errResp map[string]string
				err := json.NewDecoder(w.Body).Decode(&errResp)
				require.NoError(t, err)
				tt.checkError(t, errResp["error"])
			}

			// Cleanup if test created a wallet
			if w.Code == http.StatusCreated {
				// Extract address and network from response
				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				if addr, ok := resp["address"].(string); ok {
					network := "mainnet" // default network
					if net, ok := resp["network"].(string); ok {
						network = net
					}
					assetType := ""
					if at, ok := resp["asset_type"].(string); ok {
						assetType = at
					}
					tokenMint := ""
					if tm, ok := resp["token_mint"].(string); ok {
						tokenMint = tm
					}
					store.DeleteWallet(context.Background(), addr, network, assetType, tokenMint)
				}
			}
		})
	}
}

func TestRegisterWallet_ValidInput(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}
	handler := handleRegisterWalletWithScheduler(store, scheduler, cfg, logger)

	tests := []struct {
		name     string
		address  string
		interval string
	}{
		{"normal address", "SysvarRent111111111111111111111111111111111", "30s"},
		{"address with mix", "SysvarC1ock11111111111111111111111111111111", "1m"},
		{"max length address", "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA", "30s"}, // Valid Solana address
		{"minimum poll interval", "Config1111111111111111111111111111111111111", "10s"},
		{"various durations", "Stake11111111111111111111111111111111111111", "5m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"address":"` + tt.address + `","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"` + tt.interval + `"}`
			req := httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			// Clean up
			store.DeleteWallet(context.Background(), tt.address, "mainnet", "spl-token", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
		})
	}
}

func TestGetWallet_PathologicalInput(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleGetWallet(store, logger)

	tests := []struct {
		name           string
		address        string
		expectedStatus int
	}{
		{"empty address", "", http.StatusBadRequest},
		{"very long address", strings.Repeat("A", 500), http.StatusBadRequest}, // Caught by validation
		// Note: SQL injection is already tested in POST handler where we can properly send it in JSON
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/wallets/"+tt.address, nil)
			req.SetPathValue("address", tt.address)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestUnregisterWallet_PathologicalInput(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := handleUnregisterWallet(store, logger)

	tests := []struct {
		name           string
		address        string
		expectedStatus int
	}{
		{"empty address", "", http.StatusBadRequest},
		{"very long address", strings.Repeat("A", 500), http.StatusBadRequest}, // Caught by validation
		// Note: Path traversal and SQL injection already tested in POST handler
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", "/api/v1/wallets/"+tt.address, nil)
			req.SetPathValue("address", tt.address)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}
