package config

import (
	"os"
	"testing"
	"time"

	solanago "github.com/gagliardetto/solana-go"
)

// TestPaymentGatewayConfig_Defaults tests that payment gateway configuration
// has correct default values when environment variables are not set.
//
// WHAT IS BEING TESTED:
// We're testing the default configuration values for the payment gateway.
//
// EXPECTED BEHAVIOR:
// - Enabled should default to false (payment gateway disabled by default)
// - FeeAmount should default to 1000000 lamports (0.001 SOL)
// - FeeAssetType should default to "sol"
// - PaymentTimeout should default to 24 hours
// - MemoPrefix should default to "forohtoo-reg:"
//
// This ensures safe defaults - the payment gateway is opt-in, not opt-out.
func TestPaymentGatewayConfig_Defaults(t *testing.T) {
	// Clear any existing payment gateway env vars to test defaults
	envVars := []string{
		"PAYMENT_GATEWAY_ENABLED",
		"PAYMENT_GATEWAY_SERVICE_WALLET",
		"PAYMENT_GATEWAY_SERVICE_NETWORK",
		"PAYMENT_GATEWAY_FEE_AMOUNT",
		"PAYMENT_GATEWAY_FEE_ASSET_TYPE",
		"PAYMENT_GATEWAY_FEE_TOKEN_MINT",
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
		t.Errorf("Expected FeeAmount=1000000 (0.001 SOL), got %d", cfg.FeeAmount)
	}

	if cfg.FeeAssetType != "sol" {
		t.Errorf("Expected FeeAssetType=\"sol\", got %q", cfg.FeeAssetType)
	}

	if cfg.PaymentTimeout != 24*time.Hour {
		t.Errorf("Expected PaymentTimeout=24h, got %v", cfg.PaymentTimeout)
	}

	if cfg.MemoPrefix != "forohtoo-reg:" {
		t.Errorf("Expected MemoPrefix=\"forohtoo-reg:\", got %q", cfg.MemoPrefix)
	}
}

// TestPaymentGatewayConfig_LoadFromEnv tests that payment gateway configuration
// correctly loads values from environment variables.
//
// WHAT IS BEING TESTED:
// We're testing that all environment variables are correctly read and parsed
// into the PaymentGatewayConfig struct.
//
// EXPECTED BEHAVIOR:
// - PAYMENT_GATEWAY_ENABLED="true" should set Enabled to true
// - PAYMENT_GATEWAY_SERVICE_WALLET should be loaded as string
// - PAYMENT_GATEWAY_SERVICE_NETWORK should be loaded as string
// - PAYMENT_GATEWAY_FEE_AMOUNT should be parsed as int64
// - PAYMENT_GATEWAY_FEE_ASSET_TYPE should be loaded as string
// - PAYMENT_GATEWAY_FEE_TOKEN_MINT should be loaded as string
// - PAYMENT_GATEWAY_PAYMENT_TIMEOUT should be parsed as duration
// - PAYMENT_GATEWAY_MEMO_PREFIX should be loaded as string
//
// This ensures configuration can be properly loaded from environment variables.
func TestPaymentGatewayConfig_LoadFromEnv(t *testing.T) {
	// Set all environment variables
	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"
	testMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mainnet

	envVars := map[string]string{
		"PAYMENT_GATEWAY_ENABLED":         "true",
		"PAYMENT_GATEWAY_SERVICE_WALLET":  testWallet,
		"PAYMENT_GATEWAY_SERVICE_NETWORK": "mainnet",
		"PAYMENT_GATEWAY_FEE_AMOUNT":      "5000000", // 0.005 SOL
		"PAYMENT_GATEWAY_FEE_ASSET_TYPE":  "spl-token",
		"PAYMENT_GATEWAY_FEE_TOKEN_MINT":  testMint,
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

	if cfg.FeeAssetType != "spl-token" {
		t.Errorf("Expected FeeAssetType=\"spl-token\", got %q", cfg.FeeAssetType)
	}

	if cfg.FeeTokenMint != testMint {
		t.Errorf("Expected FeeTokenMint=%q, got %q", testMint, cfg.FeeTokenMint)
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
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method correctly enforces the requirement
// that ServiceWallet must be set when the payment gateway is enabled.
//
// EXPECTED BEHAVIOR:
// - If Enabled=true and ServiceWallet is empty, Validate() should return an error
// - The error message should clearly indicate that ServiceWallet is required
//
// This prevents misconfiguration where the payment gateway is enabled but
// the service has nowhere to receive payments.
func TestPaymentGatewayConfig_Validation_MissingServiceWallet(t *testing.T) {
	cfg := &PaymentGatewayConfig{
		Enabled:        true,
		ServiceWallet:  "", // Missing!
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		FeeAssetType:   "sol",
		PaymentTimeout: 24 * time.Hour,
		MemoPrefix:     "forohtoo-reg:",
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Expected validation error for missing ServiceWallet, got nil")
	}

	// Error message should mention ServiceWallet
	errMsg := err.Error()
	if !contains(errMsg, "ServiceWallet") {
		t.Errorf("Expected error to mention ServiceWallet, got: %v", err)
	}
}

// TestPaymentGatewayConfig_Validation_InvalidServiceWallet tests that validation
// fails when the service wallet is not a valid Solana address.
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method correctly checks that ServiceWallet
// is a valid base58-encoded Solana public key.
//
// EXPECTED BEHAVIOR:
// - If ServiceWallet is not a valid Solana address, Validate() should return an error
// - Invalid formats include: empty string, too short, too long, invalid characters
//
// This prevents configuration errors where an invalid wallet address is set,
// which would cause all payment workflows to fail.
func TestPaymentGatewayConfig_Validation_InvalidServiceWallet(t *testing.T) {
	tests := []struct {
		name    string
		wallet  string
		wantErr bool
	}{
		{
			name:    "valid wallet",
			wallet:  "FoRoHtOoWaLLeTaDdReSs1234567890123456789012", // 44 chars, valid base58
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
			name:    "invalid characters (0, O, I, l not allowed in base58)",
			wallet:  "0OIl567890123456789012345678901234567890123",
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
				FeeAssetType:   "sol",
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
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method only accepts "mainnet" or "devnet"
// as valid network values.
//
// EXPECTED BEHAVIOR:
// - "mainnet" should be valid
// - "devnet" should be valid
// - Any other value (empty, "testnet", "localnet", etc.) should fail validation
//
// This prevents misconfiguration with unsupported networks.
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
		{"random string is invalid", "foobar", true},
	}

	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: tt.network,
				FeeAmount:      1000000,
				FeeAssetType:   "sol",
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

// TestPaymentGatewayConfig_Validation_NegativeFeeAmount tests that validation
// fails when the fee amount is negative or zero.
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method enforces positive fee amounts.
//
// EXPECTED BEHAVIOR:
// - FeeAmount must be greater than 0
// - Negative values should fail validation
// - Zero should fail validation (no free registrations)
//
// This prevents misconfiguration where the service would accept invalid payments.
func TestPaymentGatewayConfig_Validation_NegativeFeeAmount(t *testing.T) {
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
				FeeAssetType:   "sol",
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

// TestPaymentGatewayConfig_Validation_ZeroTimeout tests that validation
// fails when the payment timeout is zero or negative.
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method enforces positive payment timeouts.
//
// EXPECTED BEHAVIOR:
// - PaymentTimeout must be greater than 0
// - Zero duration should fail validation
// - Negative duration should fail validation
//
// This prevents workflows from timing out immediately or having invalid timeouts.
func TestPaymentGatewayConfig_Validation_ZeroTimeout(t *testing.T) {
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
				FeeAssetType:   "sol",
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

// TestPaymentGatewayConfig_Validation_InvalidAssetType tests that validation
// fails when the fee asset type is not "sol" or "spl-token".
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method only accepts "sol" or "spl-token"
// as valid asset types.
//
// EXPECTED BEHAVIOR:
// - "sol" should be valid
// - "spl-token" should be valid
// - Any other value should fail validation
//
// This ensures we only accept supported payment types.
func TestPaymentGatewayConfig_Validation_InvalidAssetType(t *testing.T) {
	tests := []struct {
		name      string
		assetType string
		wantErr   bool
	}{
		{"sol is valid", "sol", false},
		{"spl-token is valid", "spl-token", false},
		{"empty is invalid", "", true},
		{"SOL uppercase is invalid", "SOL", true},
		{"nft is invalid", "nft", true},
	}

	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: "mainnet",
				FeeAmount:      1000000,
				FeeAssetType:   tt.assetType,
				PaymentTimeout: 24 * time.Hour,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error for asset type %q, got nil", tt.assetType)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error for asset type %q, got: %v", tt.assetType, err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_SPLTokenRequiresMint tests that validation
// fails when asset type is "spl-token" but no token mint is specified.
//
// WHAT IS BEING TESTED:
// We're testing that when FeeAssetType is "spl-token", the FeeTokenMint field
// must be provided and must be a valid Solana address.
//
// EXPECTED BEHAVIOR:
// - If FeeAssetType="spl-token" and FeeTokenMint is empty, validation should fail
// - If FeeAssetType="spl-token" and FeeTokenMint is invalid, validation should fail
// - If FeeAssetType="sol", FeeTokenMint should be ignored (can be empty)
//
// This prevents misconfiguration where SPL token payments are requested but
// no mint address is provided.
func TestPaymentGatewayConfig_Validation_SPLTokenRequiresMint(t *testing.T) {
	testWallet := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"
	validMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mainnet

	tests := []struct {
		name      string
		assetType string
		tokenMint string
		wantErr   bool
	}{
		{"sol with empty mint is valid", "sol", "", false},
		{"spl-token with valid mint is valid", "spl-token", validMint, false},
		{"spl-token with empty mint is invalid", "spl-token", "", true},
		{"spl-token with invalid mint is invalid", "spl-token", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &PaymentGatewayConfig{
				Enabled:        true,
				ServiceWallet:  testWallet,
				ServiceNetwork: "mainnet",
				FeeAmount:      1000000,
				FeeAssetType:   tt.assetType,
				FeeTokenMint:   tt.tokenMint,
				PaymentTimeout: 24 * time.Hour,
				MemoPrefix:     "forohtoo-reg:",
			}

			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Expected no validation error, got: %v", err)
			}
		})
	}
}

// TestPaymentGatewayConfig_Validation_DisabledSkipsValidation tests that when
// the payment gateway is disabled, other validation rules are not enforced.
//
// WHAT IS BEING TESTED:
// We're testing that the Validate() method skips validation of payment gateway
// fields when Enabled=false.
//
// EXPECTED BEHAVIOR:
// - If Enabled=false, validation should pass even if other fields are invalid
// - This allows the payment gateway to be safely disabled without requiring
//   all fields to be configured
//
// This prevents deployment failures when the payment gateway is intentionally
// disabled but configuration is incomplete.
func TestPaymentGatewayConfig_Validation_DisabledSkipsValidation(t *testing.T) {
	cfg := &PaymentGatewayConfig{
		Enabled:        false, // Disabled!
		ServiceWallet:  "",    // Invalid but should be ignored
		ServiceNetwork: "",    // Invalid but should be ignored
		FeeAmount:      -100,  // Invalid but should be ignored
		FeeAssetType:   "",    // Invalid but should be ignored
		PaymentTimeout: 0,     // Invalid but should be ignored
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("Expected no validation error when disabled, got: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) != -1
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Helper function to validate Solana address format
func isValidSolanaAddress(addr string) bool {
	_, err := solanago.PublicKeyFromBase58(addr)
	return err == nil
}
