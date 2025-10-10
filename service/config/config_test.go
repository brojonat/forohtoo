package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Setup environment variables
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "postgres://localhost/test", cfg.DatabaseURL)
	assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.SolanaRPCURL)
	assert.Equal(t, ":8080", cfg.ServerAddr) // Default
	assert.Equal(t, "info", cfg.LogLevel)    // Default
	assert.Equal(t, 30*time.Second, cfg.DefaultPollInterval)
	assert.Equal(t, 10*time.Second, cfg.MinPollInterval)
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestLoad_MissingSolanaRPCURL(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "SOLANA_RPC_URL is required")
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	os.Setenv("DEFAULT_POLL_INTERVAL", "invalid")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestLoad_MinIntervalGreaterThanDefault(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
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
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	os.Setenv("SOLANA_RPC_API_KEY", "secret-key")
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
	assert.Equal(t, "secret-key", cfg.SolanaRPCAPIKey)
	assert.Equal(t, time.Minute, cfg.DefaultPollInterval)
	assert.Equal(t, 15*time.Second, cfg.MinPollInterval)
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := &Config{
		DatabaseURL:         "postgres://localhost/test",
		SolanaRPCURL:        "https://api.mainnet-beta.solana.com",
		DefaultPollInterval: 30 * time.Second,
		MinPollInterval:     10 * time.Second,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestValidate_MissingDatabaseURL(t *testing.T) {
	cfg := &Config{
		SolanaRPCURL:        "https://api.mainnet-beta.solana.com",
		DefaultPollInterval: 30 * time.Second,
		MinPollInterval:     10 * time.Second,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DatabaseURL is required")
}

func TestValidate_InvalidIntervals(t *testing.T) {
	cfg := &Config{
		DatabaseURL:         "postgres://localhost/test",
		SolanaRPCURL:        "https://api.mainnet-beta.solana.com",
		DefaultPollInterval: 10 * time.Second,
		MinPollInterval:     30 * time.Second,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MinPollInterval cannot be greater than DefaultPollInterval")
}

func TestValidate_TooShortInterval(t *testing.T) {
	cfg := &Config{
		DatabaseURL:         "postgres://localhost/test",
		SolanaRPCURL:        "https://api.mainnet-beta.solana.com",
		DefaultPollInterval: 500 * time.Millisecond,
		MinPollInterval:     100 * time.Millisecond,
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
	os.Setenv("SOLANA_RPC_URL", "https://api.mainnet-beta.solana.com")
	defer cleanupEnv()

	assert.NotPanics(t, func() {
		cfg := MustLoad()
		assert.NotNil(t, cfg)
	})
}

// cleanupEnv clears all environment variables used in tests
func cleanupEnv() {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("SOLANA_RPC_URL")
	os.Unsetenv("SOLANA_RPC_API_KEY")
	os.Unsetenv("SERVER_ADDR")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("NATS_URL")
	os.Unsetenv("TEMPORAL_HOST")
	os.Unsetenv("DEFAULT_POLL_INTERVAL")
	os.Unsetenv("MIN_POLL_INTERVAL")
}
