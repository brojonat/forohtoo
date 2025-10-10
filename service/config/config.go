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

	// Solana configuration
	SolanaRPCURL string

	// Temporal configuration
	TemporalHost string

	// Polling configuration
	DefaultPollInterval time.Duration
	MinPollInterval     time.Duration
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

	// Solana configuration
	cfg.SolanaRPCURL = os.Getenv("SOLANA_RPC_URL")
	if cfg.SolanaRPCURL == "" {
		errs = append(errs, fmt.Errorf("SOLANA_RPC_URL is required"))
	}

	// Temporal configuration
	cfg.TemporalHost = getEnvOrDefault("TEMPORAL_HOST", "localhost:7233")

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

	if c.SolanaRPCURL == "" {
		errs = append(errs, fmt.Errorf("SolanaRPCURL is required"))
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
