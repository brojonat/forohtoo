package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Wallet represents a registered wallet that the server is monitoring.
type Wallet struct {
	Address      string        `json:"address"`
	PollInterval time.Duration `json:"poll_interval"`
	LastPollTime *time.Time    `json:"last_poll_time,omitempty"`
	Status       string        `json:"status"` // active, paused, error
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
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

// Register tells the server to start monitoring a wallet for transactions.
func (c *Client) Register(ctx context.Context, address string, pollInterval time.Duration) error {
	reqBody := map[string]interface{}{
		"address":       address,
		"poll_interval": pollInterval.String(),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/wallets", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return c.parseErrorResponse(resp)
	}

	c.logger.Debug("wallet registered", "address", address, "poll_interval", pollInterval)
	return nil
}

// Unregister tells the server to stop monitoring a wallet.
func (c *Client) Unregister(ctx context.Context, address string) error {
	u := fmt.Sprintf("%s/api/v1/wallets/%s", c.baseURL, url.PathEscape(address))
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

	c.logger.Debug("wallet unregistered", "address", address)
	return nil
}

// Get retrieves the registration details for a specific wallet.
func (c *Client) Get(ctx context.Context, address string) (*Wallet, error) {
	u := fmt.Sprintf("%s/api/v1/wallets/%s", c.baseURL, url.PathEscape(address))
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
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/v1/wallets", nil)
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

// walletResponse is the API response format for a wallet.
// The server returns poll_interval as a string (e.g. "30s").
type walletResponse struct {
	Address      string     `json:"address"`
	PollInterval string     `json:"poll_interval"`
	LastPollTime *time.Time `json:"last_poll_time,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// responseToWallet converts an API response to a domain Wallet.
func responseToWallet(resp *walletResponse) (*Wallet, error) {
	pollInterval, err := time.ParseDuration(resp.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid poll_interval %q: %w", resp.PollInterval, err)
	}

	return &Wallet{
		Address:      resp.Address,
		PollInterval: pollInterval,
		LastPollTime: resp.LastPollTime,
		Status:       resp.Status,
		CreatedAt:    resp.CreatedAt,
		UpdatedAt:    resp.UpdatedAt,
	}, nil
}

// Transaction represents a Solana transaction event.
type Transaction struct {
	Signature          string    `json:"signature"`
	Slot               int64     `json:"slot"`
	WalletAddress      string    `json:"wallet_address"`
	Amount             int64     `json:"amount"`
	TokenType          string    `json:"token_type"`
	Memo               string    `json:"memo,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	BlockTime          time.Time `json:"block_time"`
	ConfirmationStatus string    `json:"confirmation_status"`
	PublishedAt        time.Time `json:"published_at"`
}

// AwaitOptions defines options for waiting for a transaction.
type AwaitOptions struct {
	WorkflowID *string       // Filter by workflow_id in memo (JSON parsed)
	Signature  *string       // Filter by exact signature
	Timeout    time.Duration // How long to wait (default: 5m, max: 10m)
}

// Await blocks until a transaction matching the filter arrives, or times out.
// This is designed for payment gating in Temporal workflows - an activity can
// call this method and block until a payment arrives.
//
// Example:
//
//	workflowID := "payment-workflow-123"
//	txn, err := client.Await(ctx, walletAddress, AwaitOptions{
//	    WorkflowID: &workflowID,
//	    Timeout: 5 * time.Minute,
//	})
func (c *Client) Await(ctx context.Context, address string, opts AwaitOptions) (*Transaction, error) {
	// Build query parameters
	query := url.Values{}
	if opts.WorkflowID != nil {
		query.Set("workflow_id", *opts.WorkflowID)
	}
	if opts.Signature != nil {
		query.Set("signature", *opts.Signature)
	}
	if opts.Timeout > 0 {
		query.Set("timeout", opts.Timeout.String())
	}

	u := fmt.Sprintf("%s/api/v1/wallets/%s/await?%s", c.baseURL, url.PathEscape(address), query.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.logger.Debug("awaiting transaction",
		"address", address,
		"workflow_id", opts.WorkflowID,
		"signature", opts.Signature,
		"timeout", opts.Timeout,
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestTimeout {
		return nil, fmt.Errorf("timeout waiting for transaction")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseErrorResponse(resp)
	}

	var txn Transaction
	if err := json.NewDecoder(resp.Body).Decode(&txn); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	c.logger.Info("transaction received",
		"address", address,
		"signature", txn.Signature,
		"amount", txn.Amount,
	)

	return &txn, nil
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
