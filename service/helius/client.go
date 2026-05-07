package helius

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api-mainnet.helius-rpc.com/v0"

// Client manages Helius webhooks via the Helius API.
// It maintains a single webhook and adds/removes account addresses
// as wallets are registered/unregistered.
type Client struct {
	apiKey     string
	baseURL    string       // Helius API base URL (overridable for testing)
	webhookURL string       // Public callback URL for receiving webhook events
	authHeader string       // Auth header value sent by Helius with each webhook delivery
	httpClient *http.Client
	logger     *slog.Logger

	// Cached webhook ID, populated on EnsureWebhooks
	mainnetWebhookID string
}

// NewClient creates a new Helius API client.
func NewClient(apiKey, webhookURL, authHeader string, logger *slog.Logger) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		webhookURL: webhookURL,
		authHeader: authHeader,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// EnsureWebhooks creates or finds existing webhooks for mainnet and devnet.
// Call this on server startup to initialize webhook state.
// It returns the webhook IDs for mainnet and devnet.
func (c *Client) EnsureWebhooks(ctx context.Context) error {
	existing, err := c.ListWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("failed to list existing webhooks: %w", err)
	}

	// Find existing webhooks that match our callback URL
	for _, wh := range existing {
		if wh.WebhookURL != c.webhookURL {
			continue
		}
		// Helius doesn't include network in the webhook metadata directly,
		// but we tag our webhooks by using different callback URL paths.
		// For simplicity, we'll manage a single webhook with all addresses.
		// If we find an existing one, reuse it.
		if c.mainnetWebhookID == "" {
			c.mainnetWebhookID = wh.WebhookID
			// The LIST endpoint doesn't populate accountAddresses; SyncAddresses
			// logs the real count after a follow-up GET.
			c.logger.Info("found existing Helius webhook", "webhook_id", wh.WebhookID)
		}
	}

	// Create webhook if none exists
	if c.mainnetWebhookID == "" {
		wh, err := c.CreateWebhook(ctx, []string{})
		if err != nil {
			return fmt.Errorf("failed to create Helius webhook: %w", err)
		}
		c.mainnetWebhookID = wh.WebhookID
		c.logger.Info("created Helius webhook", "webhook_id", wh.WebhookID)
	}

	return nil
}

// WebhookID returns the active webhook ID.
func (c *Client) WebhookID() string {
	return c.mainnetWebhookID
}

// SyncAddresses ensures the webhook's address list matches the provided set.
// It fetches the current list and updates only if there's a difference.
// Call this on startup to reconcile the webhook with all active wallets from the DB.
func (c *Client) SyncAddresses(ctx context.Context, addresses []string) error {
	webhookID := c.mainnetWebhookID
	if webhookID == "" {
		return fmt.Errorf("no webhook configured; call EnsureWebhooks first")
	}

	wh, err := c.GetWebhook(ctx, webhookID)
	if err != nil {
		return fmt.Errorf("failed to get webhook: %w", err)
	}

	// Build sets for comparison
	current := make(map[string]bool, len(wh.AccountAddresses))
	for _, addr := range wh.AccountAddresses {
		current[addr] = true
	}
	desired := make(map[string]bool, len(addresses))
	for _, addr := range addresses {
		desired[addr] = true
	}

	// Check if sets are identical
	if len(current) == len(desired) {
		identical := true
		for addr := range desired {
			if !current[addr] {
				identical = false
				break
			}
		}
		if identical {
			c.logger.Info("webhook addresses already in sync", "webhook_id", webhookID, "count", len(current))
			return nil
		}
	}

	c.logger.Info("syncing webhook addresses",
		"webhook_id", webhookID,
		"current", len(current),
		"desired", len(desired),
	)

	if err := c.UpdateWebhookAddresses(ctx, webhookID, addresses); err != nil {
		return fmt.Errorf("failed to sync addresses: %w", err)
	}

	c.logger.Info("webhook addresses synced",
		"webhook_id", webhookID,
		"total_addresses", len(addresses),
	)

	return nil
}

// CreateWebhook creates a new enhanced webhook.
func (c *Client) CreateWebhook(ctx context.Context, addresses []string) (*Webhook, error) {
	reqBody := CreateWebhookRequest{
		WebhookURL:       c.webhookURL,
		TransactionTypes: []string{"Any"},
		AccountAddresses: addresses,
		WebhookType:      "enhanced",
		TxnStatus:        "success",
		AuthHeader:       c.authHeader,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/webhooks?api-key=%s", c.baseURL, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("helius API error (status %d): %s", resp.StatusCode, string(body))
	}

	var webhook Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhook); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &webhook, nil
}

// ListWebhooks returns all webhooks for this API key.
func (c *Client) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	url := fmt.Sprintf("%s/webhooks?api-key=%s", c.baseURL, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("helius API error (status %d): %s", resp.StatusCode, string(body))
	}

	var webhooks []Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhooks); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return webhooks, nil
}

// GetWebhook retrieves a webhook by ID.
func (c *Client) GetWebhook(ctx context.Context, webhookID string) (*Webhook, error) {
	url := fmt.Sprintf("%s/webhooks/%s?api-key=%s", c.baseURL, webhookID, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("helius API error (status %d): %s", resp.StatusCode, string(body))
	}

	var webhook Webhook
	if err := json.NewDecoder(resp.Body).Decode(&webhook); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &webhook, nil
}

// UpdateWebhookAddresses replaces the full account address list for the webhook.
func (c *Client) UpdateWebhookAddresses(ctx context.Context, webhookID string, addresses []string) error {
	reqBody := UpdateWebhookRequest{
		WebhookURL:       c.webhookURL,
		TransactionTypes: []string{"Any"},
		AccountAddresses: addresses,
		WebhookType:      "enhanced",
		TxnStatus:        "success",
		AuthHeader:       c.authHeader,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/webhooks/%s?api-key=%s", c.baseURL, webhookID, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("helius API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// AddAddress adds an address to the webhook's monitored account list.
// It fetches the current list, appends the new address (if not already present), and updates.
func (c *Client) AddAddress(ctx context.Context, address string) error {
	webhookID := c.mainnetWebhookID
	if webhookID == "" {
		return fmt.Errorf("no webhook configured; call EnsureWebhooks first")
	}

	wh, err := c.GetWebhook(ctx, webhookID)
	if err != nil {
		return fmt.Errorf("failed to get webhook: %w", err)
	}

	// Check if address already exists
	for _, addr := range wh.AccountAddresses {
		if addr == address {
			c.logger.Debug("address already in webhook", "address", address, "webhook_id", webhookID)
			return nil
		}
	}

	addresses := append(wh.AccountAddresses, address)
	if err := c.UpdateWebhookAddresses(ctx, webhookID, addresses); err != nil {
		return fmt.Errorf("failed to add address to webhook: %w", err)
	}

	c.logger.Info("added address to Helius webhook",
		"address", address,
		"webhook_id", webhookID,
		"total_addresses", len(addresses),
	)

	return nil
}

// RemoveAddress removes an address from the webhook's monitored account list.
func (c *Client) RemoveAddress(ctx context.Context, address string) error {
	webhookID := c.mainnetWebhookID
	if webhookID == "" {
		return fmt.Errorf("no webhook configured; call EnsureWebhooks first")
	}

	wh, err := c.GetWebhook(ctx, webhookID)
	if err != nil {
		return fmt.Errorf("failed to get webhook: %w", err)
	}

	// Filter out the address
	addresses := make([]string, 0, len(wh.AccountAddresses))
	found := false
	for _, addr := range wh.AccountAddresses {
		if addr == address {
			found = true
			continue
		}
		addresses = append(addresses, addr)
	}

	if !found {
		c.logger.Debug("address not in webhook, nothing to remove", "address", address, "webhook_id", webhookID)
		return nil
	}

	if err := c.UpdateWebhookAddresses(ctx, webhookID, addresses); err != nil {
		return fmt.Errorf("failed to remove address from webhook: %w", err)
	}

	c.logger.Info("removed address from Helius webhook",
		"address", address,
		"webhook_id", webhookID,
		"total_addresses", len(addresses),
	)

	return nil
}

// DeleteWebhook deletes a webhook by ID.
func (c *Client) DeleteWebhook(ctx context.Context, webhookID string) error {
	url := fmt.Sprintf("%s/webhooks/%s?api-key=%s", c.baseURL, webhookID, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("helius API error (status %d): %s", resp.StatusCode, string(body))
	}

	c.logger.Info("deleted Helius webhook", "webhook_id", webhookID)
	return nil
}
