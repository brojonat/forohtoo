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

	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/temporal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterWallet_CreatesTemporalSchedule(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleRegisterWalletWithScheduler(store, scheduler, logger)

	tests := []struct {
		name     string
		address  string
		network  string
		interval string
		expected time.Duration
	}{
		{
			name:     "creates schedule with 30s interval on mainnet",
			address:  "TestWa11et11111111111111111111111111111",
			network:  "mainnet",
			interval: "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "creates schedule with 5m interval on devnet",
			address:  "TestWa11et22222222222222222222222222222",
			network:  "devnet",
			interval: "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "creates schedule with 1h interval on mainnet",
			address:  "TestWa11et33333333333333333333333333333",
			network:  "mainnet",
			interval: "1h",
			expected: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"address":"%s","network":"%s","poll_interval":"%s"}`, tt.address, tt.network, tt.interval)
			req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			// Verify schedule was created
			assert.True(t, scheduler.ScheduleExists(tt.address, tt.network), "schedule should exist for wallet")

			// Verify schedule has correct interval
			interval, exists := scheduler.GetScheduleInterval(tt.address, tt.network)
			require.True(t, exists)
			assert.Equal(t, tt.expected, interval)

			// Cleanup
			store.DeleteWallet(context.Background(), tt.address, tt.network)
			scheduler.DeleteWalletSchedule(context.Background(), tt.address, tt.network)
		})
	}
}

func TestRegisterWallet_TemporalFailure(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleRegisterWalletWithScheduler(store, scheduler, logger)

	// Make scheduler return an error
	scheduler.SetCreateError(fmt.Errorf("temporal service unavailable"))

	body := `{"address":"TestWa11et44444444444444444444444444444","network":"mainnet","poll_interval":"30s"}`
	req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return error when Temporal fails
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var errResp map[string]string
	err := json.NewDecoder(w.Body).Decode(&errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp["error"], "failed to create schedule")

	// Verify wallet was not created in DB (rollback)
	exists, err := store.WalletExists(context.Background(), "TestWa11et44444444444444444444444444444", "mainnet")
	require.NoError(t, err)
	assert.False(t, exists, "wallet should not exist when schedule creation fails")
}

func TestUnregisterWallet_DeletesTemporalSchedule(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et55555555555555555555555555555"
	network := "mainnet"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		Network:      network,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletSchedule(context.Background(), address, network, 30*time.Second)
	require.NoError(t, err)

	// Verify schedule exists
	assert.True(t, scheduler.ScheduleExists(address, network))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallets/"+address+"?network="+network, nil)
	req.SetPathValue("address", address)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify schedule was deleted
	assert.False(t, scheduler.ScheduleExists(address, network), "schedule should be deleted")

	// Verify wallet was deleted from DB
	exists, err := store.WalletExists(context.Background(), address, network)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestUnregisterWallet_TemporalFailure(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et66666666666666666666666666666"
	network := "mainnet"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		Network:      network,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletSchedule(context.Background(), address, network, 30*time.Second)
	require.NoError(t, err)

	// Make scheduler return an error on delete
	scheduler.SetDeleteError(fmt.Errorf("temporal service unavailable"))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallets/"+address+"?network="+network, nil)
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
	exists, err := store.WalletExists(context.Background(), address, network)
	require.NoError(t, err)
	assert.True(t, exists, "wallet should still exist when schedule deletion fails")

	// Cleanup
	scheduler.SetDeleteError(nil)
	store.DeleteWallet(context.Background(), address, network)
	scheduler.DeleteWalletSchedule(context.Background(), address, network)
}

func TestRegisterWallet_DuplicateAddress(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleRegisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et77777777777777777777777777777"
	network := "mainnet"

	// First registration
	body := fmt.Sprintf(`{"address":"%s","network":"%s","poll_interval":"30s"}`, address, network)
	req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, scheduler.ScheduleExists(address, network))

	// Second registration (duplicate on same network)
	req = httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return conflict
	assert.Equal(t, http.StatusConflict, w.Code)

	// Should still only have one schedule
	assert.Equal(t, 1, scheduler.ScheduleCount())

	// But can register same address on different network
	bodyDevnet := fmt.Sprintf(`{"address":"%s","network":"devnet","poll_interval":"30s"}`, address)
	req = httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(bodyDevnet))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, scheduler.ScheduleExists(address, "devnet"))
	assert.Equal(t, 2, scheduler.ScheduleCount())

	// Cleanup
	store.DeleteWallet(context.Background(), address, "mainnet")
	store.DeleteWallet(context.Background(), address, "devnet")
	scheduler.DeleteWalletSchedule(context.Background(), address, "mainnet")
	scheduler.DeleteWalletSchedule(context.Background(), address, "devnet")
}
