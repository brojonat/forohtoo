package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterWallet_CreatesTemporalSchedule(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}
	handler := handleRegisterWalletAsset(store, scheduler, cfg, logger)

	tests := []struct {
		name      string
		address   string
		network   string
		tokenMint string
		interval  string
		expected  time.Duration
	}{
		{
			name:      "creates schedule with 60s interval on mainnet",
			address:   "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
			network:   "mainnet",
			tokenMint: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			interval:  "60s",
			expected:  60 * time.Second,
		},
		{
			name:      "creates schedule with 5m interval on devnet",
			address:   "SysvarRent111111111111111111111111111111111",
			network:   "devnet",
			tokenMint: "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
			interval:  "5m",
			expected:  5 * time.Minute,
		},
		{
			name:      "creates schedule with 1h interval on mainnet",
			address:   "SysvarC1ock11111111111111111111111111111111",
			network:   "mainnet",
			tokenMint: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			interval:  "1h",
			expected:  1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the new asset-aware API
			body := fmt.Sprintf(`{"address":"%s","network":"%s","asset":{"type":"spl-token","token_mint":"%s"},"poll_interval":"%s"}`, tt.address, tt.network, tt.tokenMint, tt.interval)
			req := httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			// Verify schedule was created (with asset parameters)
			assert.True(t, scheduler.ScheduleExists(tt.address, tt.network, "spl-token", tt.tokenMint), "schedule should exist for wallet asset")

			// Verify schedule has correct interval
			interval, exists := scheduler.GetScheduleInterval(tt.address, tt.network, "spl-token", tt.tokenMint)
			require.True(t, exists)
			assert.Equal(t, tt.expected, interval)

			// Cleanup
			store.DeleteWallet(context.Background(), tt.address, tt.network, "spl-token", tt.tokenMint)
			scheduler.DeleteWalletAssetSchedule(context.Background(), tt.address, tt.network, "spl-token", tt.tokenMint)
		})
	}
}

func TestRegisterWallet_TemporalFailure(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}
	handler := handleRegisterWalletAsset(store, scheduler, cfg, logger)

	// Make scheduler return an error
	scheduler.SetCreateError(fmt.Errorf("temporal service unavailable"))

	body := `{"address":"SysvarStakeHistory1111111111111111111111111","network":"mainnet","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"60s"}`
	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return error when Temporal fails
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var errResp map[string]string
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "failed to upsert schedule")

	// Verify wallet was not created in DB (rollback)
	exists, err := store.WalletExists(context.Background(), "SysvarStakeHistory1111111111111111111111111", "mainnet", "spl-token", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	require.NoError(t, err)
	assert.False(t, exists, "wallet should not exist when schedule creation fails")
}

func TestUnregisterWallet_DeletesTemporalSchedule(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletAsset(store, scheduler, logger)

	address := "Config1111111111111111111111111111111111111"
	network := "mainnet"
	assetType := "spl-token"
	tokenMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		Network:      network,
		AssetType:    assetType,
		TokenMint:    tokenMint,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletAssetSchedule(context.Background(), address, network, assetType, tokenMint, nil, 30*time.Second)
	require.NoError(t, err)

	// Verify schedule exists
	assert.True(t, scheduler.ScheduleExists(address, network, assetType, tokenMint))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallet-assets/"+address+"?network="+network+"&asset_type="+assetType+"&token_mint="+tokenMint, nil)
	req.SetPathValue("address", address)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify schedule was deleted
	assert.False(t, scheduler.ScheduleExists(address, network, assetType, tokenMint), "schedule should be deleted")

	// Verify wallet was deleted from DB
	exists, err := store.WalletExists(context.Background(), address, network, assetType, tokenMint)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestUnregisterWallet_TemporalFailure(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletAsset(store, scheduler, logger)

	address := "Stake11111111111111111111111111111111111111"
	network := "mainnet"
	assetType := "spl-token"
	tokenMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		Network:      network,
		AssetType:    assetType,
		TokenMint:    tokenMint,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletAssetSchedule(context.Background(), address, network, assetType, tokenMint, nil, 30*time.Second)
	require.NoError(t, err)

	// Make scheduler return an error on delete
	scheduler.SetDeleteError(fmt.Errorf("temporal service unavailable"))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallet-assets/"+address+"?network="+network+"&asset_type="+assetType+"&token_mint="+tokenMint, nil)
	req.SetPathValue("address", address)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return error when Temporal fails
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var errResp map[string]string
	err = json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "failed to delete schedule")

	// Verify wallet was NOT deleted from DB (rollback)
	exists, err := store.WalletExists(context.Background(), address, network, assetType, tokenMint)
	require.NoError(t, err)
	assert.True(t, exists, "wallet should still exist when schedule deletion fails")

	// Cleanup
	scheduler.SetDeleteError(nil)
	store.DeleteWallet(context.Background(), address, network, assetType, tokenMint)
	scheduler.DeleteWalletAssetSchedule(context.Background(), address, network, assetType, tokenMint)
}

func TestRegisterWallet_UpsertBehavior(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	cfg := &config.Config{
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
	}
	handler := handleRegisterWalletAsset(store, scheduler, cfg, logger)

	address := "Vote111111111111111111111111111111111111111"
	network := "mainnet"
	assetType := "spl-token"
	tokenMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	// First registration with 60s interval
	body := fmt.Sprintf(`{"address":"%s","network":"%s","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"60s"}`, address, network)
	req := httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, scheduler.ScheduleExists(address, network, assetType, tokenMint))
	interval, exists := scheduler.GetScheduleInterval(address, network, assetType, tokenMint)
	assert.True(t, exists)
	assert.Equal(t, 60*time.Second, interval)

	// Second registration (duplicate on same network) with 120s interval - should update
	bodyUpdate := fmt.Sprintf(`{"address":"%s","network":"%s","asset":{"type":"spl-token","token_mint":"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"},"poll_interval":"120s"}`, address, network)
	req = httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(bodyUpdate))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed (upsert behavior)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Should still only have one schedule
	assert.Equal(t, 1, scheduler.ScheduleCount())

	// But the interval should be updated
	interval, exists = scheduler.GetScheduleInterval(address, network, assetType, tokenMint)
	assert.True(t, exists)
	assert.Equal(t, 120*time.Second, interval)

	// Can register same address on different network
	bodyDevnet := fmt.Sprintf(`{"address":"%s","network":"devnet","asset":{"type":"spl-token","token_mint":"4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"},"poll_interval":"60s"}`, address)
	req = httptest.NewRequest("POST", "/api/v1/wallet-assets", strings.NewReader(bodyDevnet))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, scheduler.ScheduleExists(address, "devnet", "spl-token", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"))
	assert.Equal(t, 2, scheduler.ScheduleCount())

	// Cleanup
	store.DeleteWallet(context.Background(), address, "mainnet", assetType, tokenMint)
	store.DeleteWallet(context.Background(), address, "devnet", "spl-token", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	scheduler.DeleteWalletAssetSchedule(context.Background(), address, "mainnet", assetType, tokenMint)
	scheduler.DeleteWalletAssetSchedule(context.Background(), address, "devnet", "spl-token", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
}
