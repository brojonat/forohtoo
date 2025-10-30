package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
// All required fields are validated at startup to ensure fail-fast behavior.
type Config struct {
	// Server configuration
	ServerAddr string
	LogLevel   string

	// Database configuration
	DatabaseURL string

	// NATS configuration
	NATSURL string

	// Solana configuration - Mainnet
	SolanaMainnetRPCURL string
	USDCMainnetMintAddress string

	// Solana configuration - Devnet
	SolanaDevnetRPCURL string
	USDCDevnetMintAddress string

	// Temporal configuration
	TemporalHost      string
	TemporalNamespace string
	TemporalTaskQueue string

	// Polling configuration
	DefaultPollInterval time.Duration
	MinPollInterval     time.Duration

	// Payment gateway configuration
	PaymentGateway PaymentGatewayConfig
}

// PaymentGatewayConfig holds payment gateway settings for wallet registration fees.
type PaymentGatewayConfig struct {
	Enabled         bool          `json:"enabled"`           // Enable payment gateway
	ServiceWallet   string        `json:"service_wallet"`    // Forohtoo's wallet address for receiving payments
	ServiceNetwork  string        `json:"service_network"`   // "mainnet" or "devnet"
	FeeAmount       int64         `json:"fee_amount"`        // Registration fee in lamports (default: 1000000 = 0.001 SOL)
	FeeAssetType    string        `json:"fee_asset_type"`    // "sol" or "spl-token"
	FeeTokenMint    string        `json:"fee_token_mint"`    // Token mint address for SPL token fees (empty for SOL)
	PaymentTimeout  time.Duration `json:"payment_timeout"`   // How long to wait for payment (default: 24h)
	MemoPrefix      string        `json:"memo_prefix"`       // Prefix for payment memos (default: "forohtoo-reg:")
}

// Load reads configuration from environment variables and validates all required fields.
// Returns an error if any required configuration is missing or invalid.
func Load() (*Config, error) {
	cfg := &Config{}
	var errs []error

	// Server configuration
	cfg.ServerAddr = getEnvOrDefault("SERVER_ADDR", ":8080")
	cfg.LogLevel = getEnvOrDefault("LOG_LEVEL", "info")

	// Database configuration
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("DATABASE_URL is required"))
	}

	// NATS configuration
	cfg.NATSURL = getEnvOrDefault("NATS_URL", "nats://localhost:4222")

	// Solana Mainnet configuration
	cfg.SolanaMainnetRPCURL = os.Getenv("SOLANA_MAINNET_RPC_URL")
	if cfg.SolanaMainnetRPCURL == "" {
		errs = append(errs, fmt.Errorf("SOLANA_MAINNET_RPC_URL is required"))
	}

	cfg.USDCMainnetMintAddress = os.Getenv("USDC_MAINNET_MINT_ADDRESS")
	if cfg.USDCMainnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDC_MAINNET_MINT_ADDRESS is required"))
	}

	// Solana Devnet configuration
	cfg.SolanaDevnetRPCURL = os.Getenv("SOLANA_DEVNET_RPC_URL")
	if cfg.SolanaDevnetRPCURL == "" {
		errs = append(errs, fmt.Errorf("SOLANA_DEVNET_RPC_URL is required"))
	}

	cfg.USDCDevnetMintAddress = os.Getenv("USDC_DEVNET_MINT_ADDRESS")
	if cfg.USDCDevnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDC_DEVNET_MINT_ADDRESS is required"))
	}

	// Validate RPC URLs are different
	if cfg.SolanaMainnetRPCURL == cfg.SolanaDevnetRPCURL {
		errs = append(errs, fmt.Errorf("SOLANA_MAINNET_RPC_URL and SOLANA_DEVNET_RPC_URL must be different"))
	}

	// Validate USDC mint addresses are different
	if cfg.USDCMainnetMintAddress == cfg.USDCDevnetMintAddress {
		errs = append(errs, fmt.Errorf("USDC_MAINNET_MINT_ADDRESS and USDC_DEVNET_MINT_ADDRESS must be different"))
	}

	// Temporal configuration
	cfg.TemporalHost = getEnvOrDefault("TEMPORAL_HOST", "localhost:7233")
	cfg.TemporalNamespace = getEnvOrDefault("TEMPORAL_NAMESPACE", "default")
	cfg.TemporalTaskQueue = getEnvOrDefault("TEMPORAL_TASK_QUEUE", "forohtoo-wallet-polling")

	// Polling configuration
	defaultInterval, err := parseDuration("DEFAULT_POLL_INTERVAL", "30s")
	if err != nil {
		errs = append(errs, err)
	} else {
		cfg.DefaultPollInterval = defaultInterval
	}

	minInterval, err := parseDuration("MIN_POLL_INTERVAL", "10s")
	if err != nil {
		errs = append(errs, err)
	} else {
		cfg.MinPollInterval = minInterval
	}

	// Validate intervals
	if cfg.MinPollInterval > cfg.DefaultPollInterval {
		errs = append(errs, fmt.Errorf("MIN_POLL_INTERVAL (%v) cannot be greater than DEFAULT_POLL_INTERVAL (%v)",
			cfg.MinPollInterval, cfg.DefaultPollInterval))
	}

	// Payment gateway configuration
	cfg.PaymentGateway = loadPaymentGatewayConfig()
	if err := cfg.PaymentGateway.Validate(); err != nil {
		errs = append(errs, err)
	}

	// Return all validation errors
	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration validation failed: %v", errs)
	}

	return cfg, nil
}

// MustLoad is like Load but panics if configuration is invalid.
// Useful for server initialization where misconfiguration should halt startup.
func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}
	return cfg
}

// Validate checks if the configuration is valid.
// This is useful for testing configuration without loading from env.
func (c *Config) Validate() error {
	var errs []error

	if c.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("DatabaseURL is required"))
	}

	if c.SolanaMainnetRPCURL == "" {
		errs = append(errs, fmt.Errorf("SolanaMainnetRPCURL is required"))
	}

	if c.SolanaDevnetRPCURL == "" {
		errs = append(errs, fmt.Errorf("SolanaDevnetRPCURL is required"))
	}

	if c.USDCMainnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDCMainnetMintAddress is required"))
	}

	if c.USDCDevnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDCDevnetMintAddress is required"))
	}

	if c.TemporalHost == "" {
		errs = append(errs, fmt.Errorf("TemporalHost is required"))
	}

	if c.TemporalNamespace == "" {
		errs = append(errs, fmt.Errorf("TemporalNamespace is required"))
	}

	if c.TemporalTaskQueue == "" {
		errs = append(errs, fmt.Errorf("TemporalTaskQueue is required"))
	}

	if c.MinPollInterval > c.DefaultPollInterval {
		errs = append(errs, fmt.Errorf("MinPollInterval cannot be greater than DefaultPollInterval"))
	}

	if c.DefaultPollInterval < time.Second {
		errs = append(errs, fmt.Errorf("DefaultPollInterval must be at least 1 second"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed: %v", errs)
	}

	return nil
}

// getEnvOrDefault returns the environment variable value or a default if not set.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseDuration parses a duration from an environment variable or uses a default.
func parseDuration(key, defaultValue string) (time.Duration, error) {
	value := getEnvOrDefault(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, value, err)
	}
	return duration, nil
}

// parseInt parses an integer from an environment variable or uses a default.
func parseInt(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, value, err)
	}
	return result, nil
}

// GetSupportedMints returns the list of supported SPL token mint addresses for a given network.
func (c *Config) GetSupportedMints(network string) ([]string, error) {
	switch network {
	case "mainnet":
		return []string{c.USDCMainnetMintAddress}, nil
	case "devnet":
		return []string{c.USDCDevnetMintAddress}, nil
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}

// IsMintSupported checks if a mint address is supported for a given network.
func (c *Config) IsMintSupported(network string, mint string) bool {
	mints, err := c.GetSupportedMints(network)
	if err != nil {
		return false
	}
	for _, m := range mints {
		if m == mint {
			return true
		}
	}
	return false
}

// GetUSDCMintForNetwork returns the USDC mint address for a given network.
func (c *Config) GetUSDCMintForNetwork(network string) (string, error) {
	switch network {
	case "mainnet":
		return c.USDCMainnetMintAddress, nil
	case "devnet":
		return c.USDCDevnetMintAddress, nil
	default:
		return "", fmt.Errorf("unsupported network: %s", network)
	}
}

// LoadDefaults sets default values for payment gateway configuration.
func (p *PaymentGatewayConfig) LoadDefaults() {
	p.Enabled = false
	p.FeeAmount = 1000000 // 0.001 SOL
	p.FeeAssetType = "sol"
	p.PaymentTimeout = 24 * time.Hour
	p.MemoPrefix = "forohtoo-reg:"
	p.ServiceNetwork = "mainnet"
}

// LoadFromEnv loads payment gateway configuration from environment variables.
func (p *PaymentGatewayConfig) LoadFromEnv() error {
	// Load defaults first
	p.LoadDefaults()

	// Override with environment variables
	if os.Getenv("PAYMENT_GATEWAY_ENABLED") == "true" {
		p.Enabled = true
	}

	p.ServiceWallet = os.Getenv("PAYMENT_GATEWAY_SERVICE_WALLET")

	if network := os.Getenv("PAYMENT_GATEWAY_SERVICE_NETWORK"); network != "" {
		p.ServiceNetwork = network
	}

	// Parse FeeAmount
	if feeAmountStr := os.Getenv("PAYMENT_GATEWAY_FEE_AMOUNT"); feeAmountStr != "" {
		parsed, err := strconv.ParseInt(feeAmountStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid PAYMENT_GATEWAY_FEE_AMOUNT: %w", err)
		}
		p.FeeAmount = parsed
	}

	if assetType := os.Getenv("PAYMENT_GATEWAY_FEE_ASSET_TYPE"); assetType != "" {
		p.FeeAssetType = assetType
	}

	p.FeeTokenMint = os.Getenv("PAYMENT_GATEWAY_FEE_TOKEN_MINT")

	// Parse PaymentTimeout
	if timeoutStr := os.Getenv("PAYMENT_GATEWAY_PAYMENT_TIMEOUT"); timeoutStr != "" {
		parsed, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("invalid PAYMENT_GATEWAY_PAYMENT_TIMEOUT: %w", err)
		}
		p.PaymentTimeout = parsed
	}

	// Memo prefix
	if prefix := os.Getenv("PAYMENT_GATEWAY_MEMO_PREFIX"); prefix != "" {
		p.MemoPrefix = prefix
	}

	return nil
}

// loadPaymentGatewayConfig loads payment gateway configuration from environment variables.
func loadPaymentGatewayConfig() PaymentGatewayConfig {
	var cfg PaymentGatewayConfig
	cfg.LoadFromEnv() // Ignore error here, validation happens separately
	return cfg
}

// Validate checks if the PaymentGatewayConfig is valid.
func (p *PaymentGatewayConfig) Validate() error {
	if !p.Enabled {
		// If disabled, no validation needed
		return nil
	}

	var errs []error

	// ServiceWallet is required when enabled
	if p.ServiceWallet == "" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_WALLET is required when payment gateway is enabled"))
	}

	// ServiceWallet must be valid Solana address (32-44 characters, base58)
	if p.ServiceWallet != "" && (len(p.ServiceWallet) < 32 || len(p.ServiceWallet) > 44) {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_WALLET must be a valid Solana address (32-44 characters)"))
	}

	// ServiceNetwork must be mainnet or devnet
	if p.ServiceNetwork != "mainnet" && p.ServiceNetwork != "devnet" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_NETWORK must be 'mainnet' or 'devnet'"))
	}

	// FeeAmount must be positive
	if p.FeeAmount <= 0 {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_FEE_AMOUNT must be positive"))
	}

	// FeeAssetType must be sol or spl-token
	if p.FeeAssetType != "sol" && p.FeeAssetType != "spl-token" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_FEE_ASSET_TYPE must be 'sol' or 'spl-token'"))
	}

	// If FeeAssetType is spl-token, FeeTokenMint is required and must be valid
	if p.FeeAssetType == "spl-token" {
		if p.FeeTokenMint == "" {
			errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_FEE_TOKEN_MINT is required when FEE_ASSET_TYPE is 'spl-token'"))
		} else if len(p.FeeTokenMint) < 32 || len(p.FeeTokenMint) > 44 {
			// Basic validation: Solana addresses are 32-44 characters (base58)
			errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_FEE_TOKEN_MINT must be a valid Solana address (32-44 characters)"))
		}
	}

	// PaymentTimeout must be positive
	if p.PaymentTimeout <= 0 {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_PAYMENT_TIMEOUT must be positive"))
	}

	// MemoPrefix should not be empty
	if p.MemoPrefix == "" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_MEMO_PREFIX should not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("payment gateway configuration validation failed: %v", errs)
	}

	return nil
}
