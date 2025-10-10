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
		assert.Equal(t, "/api/v1/wallets", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)

		assert.Equal(t, "wallet123", body["address"])
		assert.Equal(t, "30s", body["poll_interval"])

		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.Register(context.Background(), "wallet123", 30*time.Second)
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
	err := client.Register(context.Background(), "invalid", 30*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid wallet address")
}

func TestUnregister_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/wallets/wallet123", r.URL.Path)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	err := client.Unregister(context.Background(), "wallet123")
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
	err := client.Unregister(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wallet not found")
}

func TestGet_Success(t *testing.T) {
	now := time.Now()
	lastPoll := now.Add(-5 * time.Minute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/wallets/wallet123", r.URL.Path)

		wallet := Wallet{
			Address:      "wallet123",
			PollInterval: 30 * time.Second,
			LastPollTime: &lastPoll,
			Status:       "active",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(wallet)
	}))
	defer server.Close()

	client := NewClient(server.URL, nil, nil)
	wallet, err := client.Get(context.Background(), "wallet123")
	require.NoError(t, err)
	require.NotNil(t, wallet)

	assert.Equal(t, "wallet123", wallet.Address)
	assert.Equal(t, 30*time.Second, wallet.PollInterval)
	assert.Equal(t, "active", wallet.Status)
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
	wallet, err := client.Get(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Nil(t, wallet)
	assert.Contains(t, err.Error(), "wallet not found")
}

func TestList_Success(t *testing.T) {
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/wallets", r.URL.Path)

		response := struct {
			Wallets []*Wallet `json:"wallets"`
		}{
			Wallets: []*Wallet{
				{
					Address:      "wallet123",
					PollInterval: 30 * time.Second,
					Status:       "active",
					CreatedAt:    now,
					UpdatedAt:    now,
				},
				{
					Address:      "wallet456",
					PollInterval: 60 * time.Second,
					Status:       "active",
					CreatedAt:    now,
					UpdatedAt:    now,
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
