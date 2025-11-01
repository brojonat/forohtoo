package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Wallet represents a registered wallet+asset that the server is monitoring.
type Wallet struct {
	Address                string        `json:"address"`
	Network                string        `json:"network"` // "mainnet" or "devnet"
	AssetType              string        `json:"asset_type"`
	TokenMint              string        `json:"token_mint"`
	AssociatedTokenAddress *string       `json:"associated_token_address,omitempty"`
	PollInterval           time.Duration `json:"poll_interval"`
	LastPollTime           *time.Time    `json:"last_poll_time,omitempty"`
	Status                 string        `json:"status"` // active, paused, error
	CreatedAt              time.Time     `json:"created_at"`
	UpdatedAt              time.Time     `json:"updated_at"`
}

// Client is the HTTP client for the forohtoo wallet service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new wallet service client.
func NewClient(baseURL string, httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		logger:     logger,
	}
}

// RegisterAsset tells the server to start monitoring a wallet asset for transactions.
func (c *Client) RegisterAsset(ctx context.Context, address string, network string, assetType string, tokenMint string, pollInterval time.Duration) error {
	reqBody := map[string]interface{}{
		"address": address,
		"network": network,
		"asset": map[string]interface{}{
			"type":       assetType,
			"token_mint": tokenMint,
		},
		"poll_interval": pollInterval.String(),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/wallet-assets", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 201 (Created) and 200 (OK - updated existing)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return c.parseErrorResponse(resp)
	}

	if resp.StatusCode == http.StatusOK {
		c.logger.Debug("wallet asset updated",
			"address", address,
			"asset_type", assetType,
			"token_mint", tokenMint,
			"poll_interval", pollInterval,
		)
	} else {
		c.logger.Debug("wallet asset registered",
			"address", address,
			"asset_type", assetType,
			"token_mint", tokenMint,
			"poll_interval", pollInterval,
		)
	}
	return nil
}

// UnregisterAsset tells the server to stop monitoring a wallet asset.
func (c *Client) UnregisterAsset(ctx context.Context, address string, network string, assetType string, tokenMint string) error {
	u := fmt.Sprintf("%s/api/v1/wallet-assets/%s?network=%s&asset_type=%s&token_mint=%s",
		c.baseURL,
		url.PathEscape(address),
		url.QueryEscape(network),
		url.QueryEscape(assetType),
		url.QueryEscape(tokenMint),
	)
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return c.parseErrorResponse(resp)
	}

	c.logger.Debug("wallet asset unregistered",
		"address", address,
		"asset_type", assetType,
		"token_mint", tokenMint,
	)
	return nil
}

// Get retrieves the registration details for a specific wallet.
func (c *Client) Get(ctx context.Context, address string, network string) (*Wallet, error) {
	u := fmt.Sprintf("%s/api/v1/wallet-assets/%s?network=%s", c.baseURL, url.PathEscape(address), url.QueryEscape(network))
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var apiWallet walletResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiWallet); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return responseToWallet(&apiWallet)
}

// List retrieves all registered wallets.
func (c *Client) List(ctx context.Context) ([]*Wallet, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/wallet-assets", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var response struct {
		Wallets []walletResponse `json:"wallets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert API responses to domain wallets
	wallets := make([]*Wallet, len(response.Wallets))
	for i, apiWallet := range response.Wallets {
		wallet, err := responseToWallet(&apiWallet)
		if err != nil {
			return nil, fmt.Errorf("failed to parse wallet %s: %w", apiWallet.Address, err)
		}
		wallets[i] = wallet
	}

	return wallets, nil
}

// walletResponse is the API response format for a wallet asset.
// The server returns poll_interval as a string (e.g. "30s").
type walletResponse struct {
	Address                string     `json:"address"`
	Network                string     `json:"network"`
	AssetType              string     `json:"asset_type"`
	TokenMint              string     `json:"token_mint"`
	AssociatedTokenAddress *string    `json:"associated_token_address,omitempty"`
	PollInterval           string     `json:"poll_interval"`
	LastPollTime           *time.Time `json:"last_poll_time,omitempty"`
	Status                 string     `json:"status"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// responseToWallet converts an API response to a domain Wallet.
func responseToWallet(resp *walletResponse) (*Wallet, error) {
	pollInterval, err := time.ParseDuration(resp.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid poll_interval %q: %w", resp.PollInterval, err)
	}

	return &Wallet{
		Address:                resp.Address,
		Network:                resp.Network,
		AssetType:              resp.AssetType,
		TokenMint:              resp.TokenMint,
		AssociatedTokenAddress: resp.AssociatedTokenAddress,
		PollInterval:           pollInterval,
		LastPollTime:           resp.LastPollTime,
		Status:                 resp.Status,
		CreatedAt:              resp.CreatedAt,
		UpdatedAt:              resp.UpdatedAt,
	}, nil
}

// Transaction represents a Solana transaction event.
type Transaction struct {
	Signature          string    `json:"signature"`
	Slot               int64     `json:"slot"`
	WalletAddress      string    `json:"wallet_address"`         // Destination/receiver wallet
	FromAddress        *string   `json:"from_address,omitempty"` // Source/sender wallet
	Amount             int64     `json:"amount"`
	TokenType          string    `json:"token_type"`
	Memo               *string   `json:"memo,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	BlockTime          time.Time `json:"block_time"`
	ConfirmationStatus string    `json:"confirmation_status"`
	PublishedAt        time.Time `json:"published_at"`
}

// Await blocks until a transaction matching the matcher function arrives.
// The matcher is called for each transaction received via SSE, and Await
// returns when the matcher returns true.
//
// The lookback parameter specifies how far back in time to retrieve historical
// transactions before streaming live events. If lookback is 0, only new transactions
// from the moment of connection are streamed. Historical events are limited to 1000.
//
// This is designed for payment gating in Temporal workflows - an activity can
// call this method and block until a payment arrives.
//
// Example:
//
//	// Wait for a transaction with specific memo, checking last 24 hours
//	txn, err := client.Await(ctx, walletAddress, "mainnet", 24*time.Hour, func(txn *Transaction) bool {
//	    // Check if memo contains expected workflow ID
//	    return strings.Contains(txn.Memo, "payment-workflow-123")
//	})
func (c *Client) Await(ctx context.Context, address string, network string, lookback time.Duration, matcher func(*Transaction) bool) (*Transaction, error) {
	// Build SSE stream URL
	u := fmt.Sprintf("%s/api/v1/stream/transactions/%s?network=%s", c.baseURL, url.PathEscape(address), url.QueryEscape(network))

	// Add lookback parameter if specified
	if lookback > 0 {
		u += fmt.Sprintf("&lookback=%s", url.QueryEscape(lookback.String()))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	c.logger.Debug("awaiting transaction via SSE", "address", address)

	// Create HTTP client with no timeout for streaming
	streamClient := &http.Client{
		Timeout: 0, // No timeout for SSE
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSE stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	// Parse SSE events
	return c.parseSSEStream(ctx, resp.Body, matcher)
}

// parseSSEStream parses SSE events and calls matcher on each transaction.
func (c *Client) parseSSEStream(ctx context.Context, body io.Reader, matcher func(*Transaction) bool) (*Transaction, error) {
	scanner := bufio.NewScanner(body)
	var currentEvent, currentData string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Text()

		// Empty line indicates end of event
		if line == "" {
			if currentEvent != "" && currentData != "" {
				if txn, done := c.handleSSEEvent(currentEvent, currentData, matcher); done {
					return txn, nil
				}
			}
			currentEvent = ""
			currentData = ""
			continue
		}

		// Parse event line
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			currentData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("error reading SSE stream: %w", err)
	}

	return nil, fmt.Errorf("SSE stream closed unexpectedly")
}

// handleSSEEvent processes an SSE event and returns transaction if matcher succeeds.
func (c *Client) handleSSEEvent(eventType, data string, matcher func(*Transaction) bool) (*Transaction, bool) {
	switch eventType {
	case "connected":
		c.logger.Debug("SSE stream connected")
		return nil, false

	case "transaction":
		var txn Transaction
		if err := json.Unmarshal([]byte(data), &txn); err != nil {
			c.logger.Warn("failed to unmarshal transaction", "error", err)
			return nil, false
		}

		c.logger.Debug("received transaction",
			"signature", txn.Signature,
			"amount", txn.Amount,
		)

		// Call matcher function
		if matcher(&txn) {
			c.logger.Info("transaction matched",
				"signature", txn.Signature,
				"amount", txn.Amount,
			)
			return &txn, true
		}

		return nil, false

	case "error":
		c.logger.Warn("SSE error event", "data", data)
		return nil, false

	default:
		// Unknown event type, ignore
		return nil, false
	}
}

// ListTransactions retrieves transactions for a specific wallet.
func (c *Client) ListTransactions(ctx context.Context, walletAddress string, network string, limit, offset int) ([]*Transaction, error) {
	u := fmt.Sprintf("%s/api/v1/transactions?wallet_address=%s&network=%s&limit=%d&offset=%d",
		c.baseURL,
		url.QueryEscape(walletAddress),
		url.QueryEscape(network),
		limit,
		offset,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var response struct {
		Transactions []Transaction `json:"transactions"`
		Count        int           `json:"count"`
		Limit        int           `json:"limit"`
		Offset       int           `json:"offset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to pointers for consistency with other methods
	transactions := make([]*Transaction, len(response.Transactions))
	for i := range response.Transactions {
		transactions[i] = &response.Transactions[i]
	}

	return transactions, nil
}

// parseErrorResponse attempts to parse an error response from the server.
func (c *Client) parseErrorResponse(resp *http.Response) error {
	var errResp struct {
		Error string `json:"error"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == "" {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed: %s", errResp.Error)
}
