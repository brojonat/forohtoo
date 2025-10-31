package config

import (
	"os"
	"testing"
	"time"
)

// TestPaymentGatewayConfig_Defaults tests that payment gateway configuration
// has correct default values when environment variables are not set.
//
// WHAT IS BEING TESTED:
// We're testing the default configuration values for the payment gateway.
//
// EXPECTED BEHAVIOR:
// - Enabled should default to false (payment gateway disabled by default)
// - FeeAmount should default to 1000000 (1 USDC, which has 6 decimals)
// - PaymentTimeout should default to 24 hours
// - MemoPrefix should default to "forohtoo-reg:"
//
// This ensures safe defaults - the payment gateway is opt-in, not opt-out.
// All payments are in USDC.
func TestPaymentGatewayConfig_Defaults(t *testing.T) {
	// Clear any existing payment gateway env vars to test defaults
	envVars := []string{
		"PAYMENT_GATEWAY_ENABLED",
		"PAYMENT_GATEWAY_SERVICE_WALLET",
		"PAYMENT_GATEWAY_SERVICE_NETWORK",
		"PAYMENT_GATEWAY_FEE_AMOUNT",
		"PAYMENT_GATEWAY_PAYMENT_TIMEOUT",
		"PAYMENT_GATEWAY_MEMO_PREFIX",
	}
	for _, key := range envVars {
		os.Unsetenv(key)
	}

	cfg := &PaymentGatewayConfig{}
	cfg.LoadDefaults()

	// Verify defaults
	if cfg.Enabled {
		t.Errorf("Expected Enabled=false by default, got %v", cfg.Enabled)
	}

	if cfg.FeeAmount != 1000000 {
		t.Errorf("Expected FeeAmount=1000000 (1 USDC), got %d", cfg.FeeAmount)
	}

	if cfg.PaymentTimeout != 24*time.Hour {
		t.Errorf("Expected PaymentTimeout=24h, got %v", cfg.PaymentTimeout)
	}

	if cfg.MemoPrefix != "forohtoo-reg:" {
		t.Errorf("Expected MemoPrefix=\"forohtoo-reg:\", got %q", cfg.MemoPrefix)
	}

	if cfg.ServiceNetwork != "mainnet" {
		t.Errorf("Expected ServiceNetwork=\"mainnet\", got %q", cfg.ServiceNetwork)
	}
}

// TestPaymentGatewayConfig_LoadFromEnv tests that payment gateway configuration
// correctly loads values from environment variables.
func TestPaymentGatewayConfig_LoadFromEnv(t *testing.T) {
	// Set all environment variables
	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	envVars := map[string]string{
		"PAYMENT_GATEWAY_ENABLED":         "true",
		"PAYMENT_GATEWAY_SERVICE_WALLET":  testWallet,
		"PAYMENT_GATEWAY_SERVICE_NETWORK": "mainnet",
		"PAYMENT_GATEWAY_FEE_AMOUNT":      "5000000", // 5 USDC
		"PAYMENT_GATEWAY_PAYMENT_TIMEOUT": "48h",
		"PAYMENT_GATEWAY_MEMO_PREFIX":     "custom-prefix:",
	}

	for key, value := range envVars {
		os.Setenv(key, value)
		defer os.Unsetenv(key)
	}

	cfg := &PaymentGatewayConfig{}
	err := cfg.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() failed: %v", err)
	}

	// Verify all fields loaded correctly
	if !cfg.Enabled {
		t.Errorf("Expected Enabled=true, got false")
	}

	if cfg.ServiceWallet != testWallet {
		t.Errorf("Expected ServiceWallet=%q, got %q", testWallet, cfg.ServiceWallet)
	}

	if cfg.ServiceNetwork != "mainnet" {
		t.Errorf("Expected ServiceNetwork=\"mainnet\", got %q", cfg.ServiceNetwork)
	}

	if cfg.FeeAmount != 5000000 {
		t.Errorf("Expected FeeAmount=5000000, got %d", cfg.FeeAmount)
	}

	if cfg.PaymentTimeout != 48*time.Hour {
		t.Errorf("Expected PaymentTimeout=48h, got %v", cfg.PaymentTimeout)
	}

	if cfg.MemoPrefix != "custom-prefix:" {
		t.Errorf("Expected MemoPrefix=\"custom-prefix:\", got %q", cfg.MemoPrefix)
	}
}

// TestPaymentGatewayConfig_Validation_MissingServiceWallet tests that validation
// fails when payment gateway is enabled but service wallet is not configured.
func TestPaymentGatewayConfig_Validation_MissingServiceWallet(t *testing.T) {
	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "", // Missing!
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		PaymentTimeout: 24 * time.Hour,
		MemoPrefix:     "forohtoo-reg:",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing ServiceWallet, got nil")
	}
}

// TestPaymentGatewayConfig_Validation_InvalidServiceWallet tests that validation
// fails when the service wallet is not a valid Solana address.
func TestPaymentGatewayConfig_Validation_InvalidServiceWallet(t *testing.T) {
	tests := []struct {
		name    string
		wallet  string
		wantErr bool
	}{
		{
			name:    "valid wallet",
			wallet:  "FoRoHtOoWaLLeTaDdReSs1234567890123456789012", // 44 chars
			wantErr: false,
		},
		{
			name:    "empty wallet",
			wallet:  "",
			wantErr: true,
		},
		{
			name:    "too short",
			wallet:  "abc123",
			wantErr: true,
		},
		{
			name:    "too long",
			wallet:  "FoRoHtOoWaLLeTaDdReSs12345678901234567890123Extra",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  tt.wallet,
				ServiceNetwork: "mainnet",
				FeeAmount:      1000000,
				PaymentTimeout: 24 * time.Hour,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error for wallet %q, got nil", tt.wallet)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error for wallet %q, got: %v", tt.wallet, err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_InvalidNetwork tests that validation
// fails when the service network is not "mainnet" or "devnet".
func TestPaymentGatewayConfig_Validation_InvalidNetwork(t *testing.T) {
	tests := []struct {
		name    string
		network string
		wantErr bool
	}{
		{"mainnet is valid", "mainnet", false},
		{"devnet is valid", "devnet", false},
		{"empty is invalid", "", true},
		{"testnet is invalid", "testnet", true},
		{"localnet is invalid", "localnet", true},
	}

	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: tt.network,
				FeeAmount:      1000000,
				PaymentTimeout: 24 * time.Hour,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error for network %q, got nil", tt.network)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error for network %q, got: %v", tt.network, err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_InvalidFeeAmount tests that validation
// fails when the fee amount is negative or zero.
func TestPaymentGatewayConfig_Validation_InvalidFeeAmount(t *testing.T) {
	tests := []struct {
		name      string
		feeAmount int64
		wantErr   bool
	}{
		{"positive amount is valid", 1000000, false},
		{"small positive amount is valid", 1, false},
		{"zero is invalid", 0, true},
		{"negative is invalid", -1000000, true},
	}

	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: "mainnet",
				FeeAmount:      tt.feeAmount,
				PaymentTimeout: 24 * time.Hour,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error for fee amount %d, got nil", tt.feeAmount)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error for fee amount %d, got: %v", tt.feeAmount, err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_InvalidTimeout tests that validation
// fails when the payment timeout is zero or negative.
func TestPaymentGatewayConfig_Validation_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{"positive timeout is valid", 24 * time.Hour, false},
		{"1 minute is valid", 1 * time.Minute, false},
		{"zero is invalid", 0, true},
		{"negative is invalid", -1 * time.Hour, true},
	}

	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: "mainnet",
				FeeAmount:      1000000,
				PaymentTimeout: tt.timeout,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error for timeout %v, got nil", tt.timeout)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error for timeout %v, got: %v", tt.timeout, err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_DisabledSkipsValidation tests that when
// the payment gateway is disabled, other validation rules are not enforced.
func TestPaymentGatewayConfig_Validation_DisabledSkipsValidation(t *testing.T) {
	cfg := &PaymentGatewayConfig{
		Enabled:        false, // Disabled!
		ServiceWallet:  "",    // Invalid but should be ignored
		ServiceNetwork: "",    // Invalid but should be ignored
		FeeAmount:      -100,  // Invalid but should be ignored
		PaymentTimeout: 0,     // Invalid but should be ignored
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Expected no validation error when disabled, got: %v", err)
	}
}
