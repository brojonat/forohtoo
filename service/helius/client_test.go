package helius

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCreateWebhook(t *testing.T) {
	var gotBody CreateWebhookRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/v0/webhooks")
		assert.Equal(t, "test-api-key", r.URL.Query().Get("api-key"))

		json.NewDecoder(r.Body).Decode(&gotBody)

		json.NewEncoder(w).Encode(Webhook{
			WebhookID:        "wh-123",
			WebhookURL:       gotBody.WebhookURL,
			AccountAddresses: gotBody.AccountAddresses,
			WebhookType:      gotBody.WebhookType,
		})
	}))
	defer srv.Close()

	c2 := newClientWithBaseURL(srv.URL, "test-api-key", "https://example.com/webhook", "Bearer secret", newTestLogger())

	wh, err := c2.CreateWebhook(context.Background(), []string{"addr1", "addr2"})
	require.NoError(t, err)
	assert.Equal(t, "wh-123", wh.WebhookID)
	assert.Equal(t, "enhanced", gotBody.WebhookType)
	assert.Equal(t, []string{"addr1", "addr2"}, gotBody.AccountAddresses)
	assert.Equal(t, "Bearer secret", gotBody.AuthHeader)
}

func TestListWebhooks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode([]Webhook{
			{WebhookID: "wh-1", WebhookURL: "https://example.com/webhook", AccountAddresses: []string{"a1"}},
			{WebhookID: "wh-2", WebhookURL: "https://other.com/webhook", AccountAddresses: []string{"a2"}},
		})
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	webhooks, err := c.ListWebhooks(context.Background())
	require.NoError(t, err)
	assert.Len(t, webhooks, 2)
}

func TestEnsureWebhooks_FindsExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]Webhook{
				{WebhookID: "existing-wh", WebhookURL: "https://example.com/webhook"},
			})
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	err := c.EnsureWebhooks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "existing-wh", c.WebhookID())
}

func TestEnsureWebhooks_CreatesNew(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode([]Webhook{}) // No existing webhooks
			return
		}
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(Webhook{WebhookID: "new-wh"})
			return
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	err := c.EnsureWebhooks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "new-wh", c.WebhookID())
	assert.Equal(t, 2, callCount) // GET list + POST create
}

func TestAddAddress(t *testing.T) {
	var gotAddresses []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v0/webhooks/wh-1") {
			json.NewEncoder(w).Encode(Webhook{
				WebhookID:        "wh-1",
				AccountAddresses: []string{"existing-addr"},
			})
			return
		}
		if r.Method == http.MethodPut {
			var body UpdateWebhookRequest
			json.NewDecoder(r.Body).Decode(&body)
			gotAddresses = body.AccountAddresses
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	c.mainnetWebhookID = "wh-1"

	err := c.AddAddress(context.Background(), "new-addr")
	require.NoError(t, err)
	assert.Equal(t, []string{"existing-addr", "new-addr"}, gotAddresses)
}

func TestAddAddress_AlreadyExists(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(Webhook{
				WebhookID:        "wh-1",
				AccountAddresses: []string{"already-there"},
			})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	c.mainnetWebhookID = "wh-1"

	err := c.AddAddress(context.Background(), "already-there")
	require.NoError(t, err)
	assert.False(t, putCalled, "should not call PUT when address already exists")
}

func TestRemoveAddress(t *testing.T) {
	var gotAddresses []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(Webhook{
				WebhookID:        "wh-1",
				AccountAddresses: []string{"keep-me", "remove-me", "also-keep"},
			})
			return
		}
		if r.Method == http.MethodPut {
			var body UpdateWebhookRequest
			json.NewDecoder(r.Body).Decode(&body)
			gotAddresses = body.AccountAddresses
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	c.mainnetWebhookID = "wh-1"

	err := c.RemoveAddress(context.Background(), "remove-me")
	require.NoError(t, err)
	assert.Equal(t, []string{"keep-me", "also-keep"}, gotAddresses)
}

func TestRemoveAddress_NotFound(t *testing.T) {
	putCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(Webhook{
				WebhookID:        "wh-1",
				AccountAddresses: []string{"some-addr"},
			})
			return
		}
		if r.Method == http.MethodPut {
			putCalled = true
		}
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	c.mainnetWebhookID = "wh-1"

	err := c.RemoveAddress(context.Background(), "not-in-list")
	require.NoError(t, err)
	assert.False(t, putCalled, "should not call PUT when address not in list")
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	c := newClientWithBaseURL(srv.URL, "key", "https://example.com/webhook", "Bearer s", newTestLogger())
	_, err := c.ListWebhooks(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}

// newClientWithBaseURL creates a Client that talks to a custom base URL (for testing).
func newClientWithBaseURL(base, apiKey, webhookURL, authHeader string, logger *slog.Logger) *Client {
	c := NewClient(apiKey, webhookURL, authHeader, logger)
	c.baseURL = base + "/v0"
	return c
}
