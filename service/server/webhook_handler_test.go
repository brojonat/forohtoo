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

	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPublisher records published events for testing.
type mockPublisher struct {
	events []*natspkg.TransactionEvent
}

func (m *mockPublisher) PublishTransaction(_ context.Context, event *natspkg.TransactionEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockPublisher) PublishTransactionBatch(_ context.Context, events []*natspkg.TransactionEvent) error {
	m.events = append(m.events, events...)
	return nil
}

func (m *mockPublisher) Close() error { return nil }

func webhookTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestWebhookHandler_AuthRequired(t *testing.T) {
	handler := handleHeliusWebhook(nil, nil, "Bearer my-secret", webhookTestLogger())

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"no auth header", "", http.StatusUnauthorized},
		{"wrong auth header", "Bearer wrong-secret", http.StatusUnauthorized},
		{"correct auth header", "Bearer my-secret", http.StatusBadRequest}, // passes auth, fails on empty body
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(""))
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestWebhookHandler_EmptyPayload(t *testing.T) {
	handler := handleHeliusWebhook(nil, nil, "secret", webhookTestLogger())

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader("[]"))
	req.Header.Set("Authorization", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	handler := handleHeliusWebhook(nil, nil, "secret", webhookTestLogger())

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader("not json at all"))
	req.Header.Set("Authorization", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWebhookHandler_ValidPayload_NoMatchingWallets(t *testing.T) {
	// Use a nil store - buildAddressMap will fail, but we test that
	// the handler returns 500 for the DB error.
	// For a unit test without a real DB, we test the flow up to address map building.
	handler := handleHeliusWebhook(nil, nil, "secret", webhookTestLogger())

	payload := mustJSON(t, []map[string]interface{}{
		{
			"signature":        "sig123",
			"slot":             100000,
			"timestamp":        1700000000,
			"fee":              5000,
			"feePayer":         "sender",
			"nativeTransfers":  []map[string]interface{}{},
			"tokenTransfers":   []map[string]interface{}{},
			"transactionError": nil,
		},
	})

	req := httptest.NewRequest("POST", "/api/v1/webhooks/helius", strings.NewReader(payload))
	req.Header.Set("Authorization", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// With nil store, buildAddressMap will panic or return error
	// The handler should return 500
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestBuildAddressMap_NilStore(t *testing.T) {
	// buildAddressMap with nil store should return an error
	_, err := buildAddressMap(context.Background(), nil)
	assert.Error(t, err)
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}
