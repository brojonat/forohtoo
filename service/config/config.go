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

	// USDC mint addresses per network (used to compute the ATA we monitor for
	// payment-gated registrations and to validate registration requests).
	USDCMainnetMintAddress string
	USDCDevnetMintAddress  string

	// Temporal configuration (only used when payment gateway is enabled)
	TemporalHost      string
	TemporalNamespace string
	TemporalTaskQueue string

	// Helius webhook configuration (the only ingestion path)
	HeliusAPIKey           string
	HeliusWebhookURL       string
	HeliusWebhookAuthToken string

	// Payment gateway configuration
	PaymentGateway PaymentGatewayConfig
}

// PaymentGatewayConfig holds payment gateway settings for wallet registration fees.
type PaymentGatewayConfig struct {
	Enabled        bool          `json:"enabled"`
	ServiceWallet  string        `json:"service_wallet"`
	ServiceNetwork string        `json:"service_network"`
	FeeAmount      int64         `json:"fee_amount"`
	PaymentTimeout time.Duration `json:"payment_timeout"`
	MemoPrefix     string        `json:"memo_prefix"`
}

// Load reads configuration from environment variables and validates required fields.
func Load() (*Config, error) {
	cfg := &Config{}
	var errs []error

	cfg.ServerAddr = getEnvOrDefault("SERVER_ADDR", ":8080")
	cfg.LogLevel = getEnvOrDefault("LOG_LEVEL", "info")

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("DATABASE_URL is required"))
	}

	cfg.NATSURL = getEnvOrDefault("NATS_URL", "nats://localhost:4222")

	cfg.USDCMainnetMintAddress = os.Getenv("USDC_MAINNET_MINT_ADDRESS")
	if cfg.USDCMainnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDC_MAINNET_MINT_ADDRESS is required"))
	}

	cfg.USDCDevnetMintAddress = os.Getenv("USDC_DEVNET_MINT_ADDRESS")
	if cfg.USDCDevnetMintAddress == "" {
		errs = append(errs, fmt.Errorf("USDC_DEVNET_MINT_ADDRESS is required"))
	}

	if cfg.USDCMainnetMintAddress != "" && cfg.USDCMainnetMintAddress == cfg.USDCDevnetMintAddress {
		errs = append(errs, fmt.Errorf("USDC_MAINNET_MINT_ADDRESS and USDC_DEVNET_MINT_ADDRESS must be different"))
	}

	cfg.HeliusAPIKey = os.Getenv("HELIUS_API_KEY")
	if cfg.HeliusAPIKey == "" {
		errs = append(errs, fmt.Errorf("HELIUS_API_KEY is required"))
	}
	cfg.HeliusWebhookURL = os.Getenv("HELIUS_WEBHOOK_URL")
	if cfg.HeliusWebhookURL == "" {
		errs = append(errs, fmt.Errorf("HELIUS_WEBHOOK_URL is required"))
	}
	cfg.HeliusWebhookAuthToken = os.Getenv("HELIUS_WEBHOOK_AUTH_TOKEN")
	if cfg.HeliusWebhookAuthToken == "" {
		errs = append(errs, fmt.Errorf("HELIUS_WEBHOOK_AUTH_TOKEN is required"))
	}

	cfg.TemporalHost = getEnvOrDefault("TEMPORAL_HOST", "localhost:7233")
	cfg.TemporalNamespace = getEnvOrDefault("TEMPORAL_NAMESPACE", "default")
	cfg.TemporalTaskQueue = getEnvOrDefault("TEMPORAL_TASK_QUEUE", "forohtoo-payment-gateway")

	cfg.PaymentGateway = loadPaymentGatewayConfig()
	if err := cfg.PaymentGateway.Validate(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration validation failed: %v", errs)
	}

	return cfg, nil
}

// MustLoad is like Load but panics if configuration is invalid.
func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}
	return cfg
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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
	p.FeeAmount = 1000000 // 1 USDC (USDC has 6 decimals)
	p.PaymentTimeout = 24 * time.Hour
	p.MemoPrefix = "forohtoo-reg:"
	p.ServiceNetwork = "mainnet"
}

// LoadFromEnv loads payment gateway configuration from environment variables.
func (p *PaymentGatewayConfig) LoadFromEnv() error {
	p.LoadDefaults()

	if os.Getenv("PAYMENT_GATEWAY_ENABLED") == "true" {
		p.Enabled = true
	}

	p.ServiceWallet = os.Getenv("PAYMENT_GATEWAY_SERVICE_WALLET")

	if network := os.Getenv("PAYMENT_GATEWAY_SERVICE_NETWORK"); network != "" {
		p.ServiceNetwork = network
	}

	if feeAmountStr := os.Getenv("PAYMENT_GATEWAY_FEE_AMOUNT"); feeAmountStr != "" {
		parsed, err := strconv.ParseInt(feeAmountStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid PAYMENT_GATEWAY_FEE_AMOUNT: %w", err)
		}
		p.FeeAmount = parsed
	}

	if timeoutStr := os.Getenv("PAYMENT_GATEWAY_PAYMENT_TIMEOUT"); timeoutStr != "" {
		parsed, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("invalid PAYMENT_GATEWAY_PAYMENT_TIMEOUT: %w", err)
		}
		p.PaymentTimeout = parsed
	}

	if prefix := os.Getenv("PAYMENT_GATEWAY_MEMO_PREFIX"); prefix != "" {
		p.MemoPrefix = prefix
	}

	return nil
}

func loadPaymentGatewayConfig() PaymentGatewayConfig {
	var cfg PaymentGatewayConfig
	_ = cfg.LoadFromEnv()
	return cfg
}

// Validate checks if the PaymentGatewayConfig is valid.
func (p *PaymentGatewayConfig) Validate() error {
	if !p.Enabled {
		return nil
	}

	var errs []error

	if p.ServiceWallet == "" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_WALLET is required when payment gateway is enabled"))
	}
	if p.ServiceWallet != "" && (len(p.ServiceWallet) < 32 || len(p.ServiceWallet) > 44) {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_WALLET must be a valid Solana address (32-44 characters)"))
	}
	if p.ServiceNetwork != "mainnet" && p.ServiceNetwork != "devnet" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_SERVICE_NETWORK must be 'mainnet' or 'devnet'"))
	}
	if p.FeeAmount <= 0 {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_FEE_AMOUNT must be positive"))
	}
	if p.PaymentTimeout <= 0 {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_PAYMENT_TIMEOUT must be positive"))
	}
	if p.MemoPrefix == "" {
		errs = append(errs, fmt.Errorf("PAYMENT_GATEWAY_MEMO_PREFIX should not be empty"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("payment gateway configuration validation failed: %v", errs)
	}

	return nil
}
