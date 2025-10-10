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
		interval string
		expected time.Duration
	}{
		{
			name:     "creates schedule with 30s interval",
			address:  "TestWa11et11111111111111111111111111111",
			interval: "30s",
			expected: 30 * time.Second,
		},
		{
			name:     "creates schedule with 5m interval",
			address:  "TestWa11et22222222222222222222222222222",
			interval: "5m",
			expected: 5 * time.Minute,
		},
		{
			name:     "creates schedule with 1h interval",
			address:  "TestWa11et33333333333333333333333333333",
			interval: "1h",
			expected: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"address":"%s","poll_interval":"%s"}`, tt.address, tt.interval)
			req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			// Verify schedule was created
			assert.True(t, scheduler.ScheduleExists(tt.address), "schedule should exist for wallet")

			// Verify schedule has correct interval
			interval, exists := scheduler.GetScheduleInterval(tt.address)
			require.True(t, exists)
			assert.Equal(t, tt.expected, interval)

			// Cleanup
			store.DeleteWallet(context.Background(), tt.address)
			scheduler.DeleteWalletSchedule(context.Background(), tt.address)
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

	body := `{"address":"TestWa11et44444444444444444444444444444","poll_interval":"30s"}`
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
	exists, err := store.WalletExists(context.Background(), "TestWa11et44444444444444444444444444444")
	require.NoError(t, err)
	assert.False(t, exists, "wallet should not exist when schedule creation fails")
}

func TestUnregisterWallet_DeletesTemporalSchedule(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et55555555555555555555555555555"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletSchedule(context.Background(), address, 30*time.Second)
	require.NoError(t, err)

	// Verify schedule exists
	assert.True(t, scheduler.ScheduleExists(address))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallets/"+address, nil)
	req.SetPathValue("address", address)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify schedule was deleted
	assert.False(t, scheduler.ScheduleExists(address), "schedule should be deleted")

	// Verify wallet was deleted from DB
	exists, err := store.WalletExists(context.Background(), address)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestUnregisterWallet_TemporalFailure(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleUnregisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et66666666666666666666666666666"

	// Create wallet and schedule
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      address,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	err = scheduler.CreateWalletSchedule(context.Background(), address, 30*time.Second)
	require.NoError(t, err)

	// Make scheduler return an error on delete
	scheduler.SetDeleteError(fmt.Errorf("temporal service unavailable"))

	// Unregister wallet
	req := httptest.NewRequest("DELETE", "/api/v1/wallets/"+address, nil)
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
	exists, err := store.WalletExists(context.Background(), address)
	require.NoError(t, err)
	assert.True(t, exists, "wallet should still exist when schedule deletion fails")

	// Cleanup
	scheduler.SetDeleteError(nil)
	store.DeleteWallet(context.Background(), address)
	scheduler.DeleteWalletSchedule(context.Background(), address)
}

func TestRegisterWallet_DuplicateAddress(t *testing.T) {
	store := setupTestStore(t)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	scheduler := temporal.NewMockScheduler()
	handler := handleRegisterWalletWithScheduler(store, scheduler, logger)

	address := "TestWa11et77777777777777777777777777777"

	// First registration
	body := fmt.Sprintf(`{"address":"%s","poll_interval":"30s"}`, address)
	req := httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, scheduler.ScheduleExists(address))

	// Second registration (duplicate)
	req = httptest.NewRequest("POST", "/api/v1/wallets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return conflict
	assert.Equal(t, http.StatusConflict, w.Code)

	// Should still only have one schedule
	assert.Equal(t, 1, scheduler.ScheduleCount())

	// Cleanup
	store.DeleteWallet(context.Background(), address)
	scheduler.DeleteWalletSchedule(context.Background(), address)
}
