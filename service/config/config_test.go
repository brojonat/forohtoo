package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Clean up any existing env vars first
	cleanupEnv()

	// Setup environment variables
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "postgres://localhost/test", cfg.DatabaseURL)
	assert.Len(t, cfg.SolanaMainnetRPCURLs, 1)
	assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.SolanaMainnetRPCURLs[0])
	assert.Len(t, cfg.SolanaDevnetRPCURLs, 1)
	assert.Equal(t, "https://api.devnet.solana.com", cfg.SolanaDevnetRPCURLs[0])
	assert.Equal(t, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", cfg.USDCMainnetMintAddress)
	assert.Equal(t, "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", cfg.USDCDevnetMintAddress)
	assert.Equal(t, ":8080", cfg.ServerAddr) // Default
	assert.Equal(t, "info", cfg.LogLevel)    // Default
	assert.Equal(t, 30*time.Second, cfg.DefaultPollInterval)
	assert.Equal(t, 10*time.Second, cfg.MinPollInterval)
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestLoad_MissingMainnetRPCURL(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "SOLANA_MAINNET_RPC_URLS is required")
}

func TestLoad_MissingDevnetRPCURL(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "SOLANA_DEVNET_RPC_URLS is required")
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	os.Setenv("DEFAULT_POLL_INTERVAL", "invalid")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestLoad_MinIntervalGreaterThanDefault(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	os.Setenv("DEFAULT_POLL_INTERVAL", "10s")
	os.Setenv("MIN_POLL_INTERVAL", "30s")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "cannot be greater than")
}

func TestLoad_CustomValues(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	os.Setenv("SERVER_ADDR", ":9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("NATS_URL", "nats://nats.example.com:4222")
	os.Setenv("TEMPORAL_HOST", "temporal.example.com:7233")
	os.Setenv("DEFAULT_POLL_INTERVAL", "1m")
	os.Setenv("MIN_POLL_INTERVAL", "15s")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, ":9090", cfg.ServerAddr)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "nats://nats.example.com:4222", cfg.NATSURL)
	assert.Equal(t, "temporal.example.com:7233", cfg.TemporalHost)
	assert.Equal(t, "default", cfg.TemporalNamespace)
	assert.Equal(t, "forohtoo-wallet-polling", cfg.TemporalTaskQueue)
	assert.Equal(t, time.Minute, cfg.DefaultPollInterval)
	assert.Equal(t, 15*time.Second, cfg.MinPollInterval)
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		DatabaseURL:            "postgres://localhost/test",
		SolanaMainnetRPCURLs:   []string{"https://api.mainnet-beta.solana.com"},
		SolanaDevnetRPCURLs:    []string{"https://api.devnet.solana.com"},
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
		TemporalHost:           "localhost:7233",
		TemporalNamespace:      "default",
		TemporalTaskQueue:      "forohtoo-wallet-polling",
		DefaultPollInterval:    30 * time.Second,
		MinPollInterval:        10 * time.Second,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_MissingDatabaseURL(t *testing.T) {
	cfg := &Config{
		SolanaMainnetRPCURLs:   []string{"https://api.mainnet-beta.solana.com"},
		SolanaDevnetRPCURLs:    []string{"https://api.devnet.solana.com"},
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
		TemporalHost:           "localhost:7233",
		TemporalNamespace:      "default",
		TemporalTaskQueue:      "forohtoo-wallet-polling",
		DefaultPollInterval:    30 * time.Second,
		MinPollInterval:        10 * time.Second,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DatabaseURL is required")
}

func TestValidate_InvalidIntervals(t *testing.T) {
	cfg := &Config{
		DatabaseURL:            "postgres://localhost/test",
		SolanaMainnetRPCURLs:   []string{"https://api.mainnet-beta.solana.com"},
		SolanaDevnetRPCURLs:    []string{"https://api.devnet.solana.com"},
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
		TemporalHost:           "localhost:7233",
		TemporalNamespace:      "default",
		TemporalTaskQueue:      "forohtoo-wallet-polling",
		DefaultPollInterval:    10 * time.Second,
		MinPollInterval:        30 * time.Second,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MinPollInterval cannot be greater than DefaultPollInterval")
}

func TestValidate_TooShortInterval(t *testing.T) {
	cfg := &Config{
		DatabaseURL:            "postgres://localhost/test",
		SolanaMainnetRPCURLs:   []string{"https://api.mainnet-beta.solana.com"},
		SolanaDevnetRPCURLs:    []string{"https://api.devnet.solana.com"},
		USDCMainnetMintAddress: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		USDCDevnetMintAddress:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
		TemporalHost:           "localhost:7233",
		TemporalNamespace:      "default",
		TemporalTaskQueue:      "forohtoo-wallet-polling",
		DefaultPollInterval:    500 * time.Millisecond,
		MinPollInterval:        100 * time.Millisecond,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be at least 1 second")
}

func TestMustLoad_Panics(t *testing.T) {
	// Don't set required env vars
	defer cleanupEnv()

	assert.Panics(t, func() {
		MustLoad()
	})
}

func TestMustLoad_Success(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	assert.NotPanics(t, func() {
		cfg := MustLoad()
		assert.NotNil(t, cfg)
	})
}

// Multi-endpoint tests

func TestLoad_MultipleMainnetEndpoints(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com,https://rpc.ankr.com/solana")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.SolanaMainnetRPCURLs, 3)
	assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.SolanaMainnetRPCURLs[0])
	assert.Equal(t, "https://mainnet.helius-rpc.com", cfg.SolanaMainnetRPCURLs[1])
	assert.Equal(t, "https://rpc.ankr.com/solana", cfg.SolanaMainnetRPCURLs[2])
}

func TestLoad_EndpointsWithWhitespace(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_MAINNET_RPC_URLS", "  https://api.mainnet-beta.solana.com  ,  https://mainnet.helius-rpc.com  ,  https://rpc.ankr.com/solana  ")
	os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Len(t, cfg.SolanaMainnetRPCURLs, 3)
	assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.SolanaMainnetRPCURLs[0])
	assert.Equal(t, "https://mainnet.helius-rpc.com", cfg.SolanaMainnetRPCURLs[1])
	assert.Equal(t, "https://rpc.ankr.com/solana", cfg.SolanaMainnetRPCURLs[2])
}

func TestParseEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "multiple endpoints",
			input:    "https://a.com,https://b.com,https://c.com",
			expected: []string{"https://a.com", "https://b.com", "https://c.com"},
		},
		{
			name:     "single endpoint",
			input:    "https://api.mainnet-beta.solana.com",
			expected: []string{"https://api.mainnet-beta.solana.com"},
		},
		{
			name:     "endpoints with whitespace",
			input:    "  https://a.com  ,  https://b.com  ,  https://c.com  ",
			expected: []string{"https://a.com", "https://b.com", "https://c.com"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "only whitespace",
			input:    "   ,   ,   ",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEndpoints(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// cleanupEnv clears all environment variables used in tests
func cleanupEnv() {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("SOLANA_MAINNET_RPC_URLS")
	os.Unsetenv("SOLANA_DEVNET_RPC_URLS")
	os.Unsetenv("USDC_MAINNET_MINT_ADDRESS")
	os.Unsetenv("USDC_DEVNET_MINT_ADDRESS")
	os.Unsetenv("SERVER_ADDR")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("NATS_URL")
	os.Unsetenv("TEMPORAL_HOST")
	os.Unsetenv("TEMPORAL_NAMESPACE")
	os.Unsetenv("TEMPORAL_TASK_QUEUE")
	os.Unsetenv("DEFAULT_POLL_INTERVAL")
	os.Unsetenv("MIN_POLL_INTERVAL")
}
