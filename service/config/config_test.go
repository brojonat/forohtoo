package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setRequiredEnv() {
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	os.Setenv("USDC_MAINNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	os.Setenv("HELIUS_API_KEY", "test-helius-key")
	os.Setenv("HELIUS_WEBHOOK_URL", "https://example.com/api/v1/webhooks/helius")
	os.Setenv("HELIUS_WEBHOOK_AUTH_TOKEN", "Bearer test-secret")
}

func TestLoad_ValidConfig(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "postgres://localhost/test", cfg.DatabaseURL)
	assert.Equal(t, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", cfg.USDCMainnetMintAddress)
	assert.Equal(t, "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", cfg.USDCDevnetMintAddress)
	assert.Equal(t, ":8080", cfg.ServerAddr)
	assert.Equal(t, "info", cfg.LogLevel)
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	os.Unsetenv("DATABASE_URL")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestLoad_MissingHeliusKey(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	os.Unsetenv("HELIUS_API_KEY")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "HELIUS_API_KEY is required")
}

func TestLoad_CustomValues(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	os.Setenv("SERVER_ADDR", ":9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("NATS_URL", "nats://nats.example.com:4222")
	os.Setenv("TEMPORAL_HOST", "temporal.example.com:7233")
	defer cleanupEnv()

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, ":9090", cfg.ServerAddr)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "nats://nats.example.com:4222", cfg.NATSURL)
	assert.Equal(t, "temporal.example.com:7233", cfg.TemporalHost)
	assert.Equal(t, "default", cfg.TemporalNamespace)
	assert.Equal(t, "forohtoo-payment-gateway", cfg.TemporalTaskQueue)
}

func TestLoad_USDCMintsMustDiffer(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	os.Setenv("USDC_DEVNET_MINT_ADDRESS", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	defer cleanupEnv()

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "must be different")
}

func TestMustLoad_Panics(t *testing.T) {
	cleanupEnv()
	defer cleanupEnv()

	assert.Panics(t, func() {
		MustLoad()
	})
}

func TestMustLoad_Success(t *testing.T) {
	cleanupEnv()
	setRequiredEnv()
	defer cleanupEnv()

	assert.NotPanics(t, func() {
		cfg := MustLoad()
		assert.NotNil(t, cfg)
	})
}

func cleanupEnv() {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("USDC_MAINNET_MINT_ADDRESS")
	os.Unsetenv("USDC_DEVNET_MINT_ADDRESS")
	os.Unsetenv("SERVER_ADDR")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("NATS_URL")
	os.Unsetenv("TEMPORAL_HOST")
	os.Unsetenv("TEMPORAL_NAMESPACE")
	os.Unsetenv("TEMPORAL_TASK_QUEUE")
	os.Unsetenv("HELIUS_API_KEY")
	os.Unsetenv("HELIUS_WEBHOOK_URL")
	os.Unsetenv("HELIUS_WEBHOOK_AUTH_TOKEN")
}
