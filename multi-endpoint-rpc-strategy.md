# Multi-Endpoint RPC Strategy for Solana

## Problem Statement

Currently, Solana RPC access is configured with a single endpoint. This creates several issues:

1. **Rate Limiting**: Single provider rate limits can block operations
2. **Cost Scaling**: As usage grows, costs increase linearly (e.g., Helius paid tiers)
3. **Single Point of Failure**: If the provider has downtime, all operations fail
4. **Vendor Lock-in**: Switching providers requires code/config changes

## Proposed Solution: Random Endpoint Selection

Distribute RPC queries across multiple providers using random selection.

### Why Random Selection (vs Round-Robin)?

In a **stateless, distributed system** (like Temporal workflows), random selection is superior:

| Approach | Pros | Cons |
|----------|------|------|
| **Random** | ✅ Stateless<br>✅ Works across distributed workers<br>✅ Good distribution over many calls<br>✅ Simple implementation | ❌ Not perfectly uniform |
| **Round-Robin** | ✅ Perfectly uniform distribution | ❌ Requires shared state (Redis/DB)<br>❌ Complex in distributed systems<br>❌ Adds latency and failure points |

For workflow activities that execute across multiple workers, random selection achieves similar distribution without coordination overhead.

## Implementation Plan for Forohtoo Project

### Project-Specific Context

This project has two Solana networks configured:
- **Mainnet**: Production network with paid RPC endpoints (Helius)
- **Devnet**: Development/testing network with free endpoints

Configuration is centralized in `service/config/config.go` and loaded from environment variables.

### 1. Update Configuration Structure

**File:** `service/config/config.go`

**Current (lines 25-30):**
```go
// Solana configuration - Mainnet
SolanaMainnetRPCURL string
USDCMainnetMintAddress string

// Solana configuration - Devnet
SolanaDevnetRPCURL string
USDCDevnetMintAddress string
```

**Proposed:**
```go
// Solana configuration - Mainnet (multiple endpoints for load distribution)
SolanaMainnetRPCURLs []string
USDCMainnetMintAddress string

// Solana configuration - Devnet (multiple endpoints for load distribution)
SolanaDevnetRPCURLs []string
USDCDevnetMintAddress string
```

### 2. Environment Variable Format

**Files:** `.env.worker.example`, `.env.server.example`

**Current:**
```bash
SOLANA_MAINNET_RPC_URL=https://api.mainnet-beta.solana.com
SOLANA_DEVNET_RPC_URL=https://api.devnet.solana.com
```

**Proposed:**
```bash
# Solana Mainnet RPC Endpoints (comma-separated list)
# Mix free public endpoints with paid services to distribute load
SOLANA_MAINNET_RPC_URLS=https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com,https://rpc.ankr.com/solana

# Solana Devnet RPC Endpoints (comma-separated list)
SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com
```

Parse as comma-separated list, trim whitespace from each endpoint.

### 3. Create Endpoint Selector

**File:** `service/solana/adapter.go`

Add utility function before `NewRPCClient()` (around line 18):

```go
// SelectRandomEndpoint picks a random endpoint from the pool.
// Returns error if endpoints slice is empty.
func SelectRandomEndpoint(endpoints []string) (string, error) {
    if len(endpoints) == 0 {
        return "", fmt.Errorf("no RPC endpoints configured")
    }
    return endpoints[rand.Intn(len(endpoints))], nil
}
```

**Note:** Go 1.20+ automatically seeds `rand.Intn()`, no manual seeding needed.

### 4. Update Configuration Loading

**File:** `service/config/config.go`

**Update the `Load()` function (lines 77-91):**

Replace the mainnet single URL loading:
```go
// OLD (lines 77-80):
cfg.SolanaMainnetRPCURL = os.Getenv("SOLANA_MAINNET_RPC_URL")
if cfg.SolanaMainnetRPCURL == "" {
    errs = append(errs, fmt.Errorf("SOLANA_MAINNET_RPC_URL is required"))
}

// NEW:
mainnetURLsStr := os.Getenv("SOLANA_MAINNET_RPC_URLS")
if mainnetURLsStr == "" {
    errs = append(errs, fmt.Errorf("SOLANA_MAINNET_RPC_URLS is required"))
}
cfg.SolanaMainnetRPCURLs = parseEndpoints(mainnetURLsStr)
if len(cfg.SolanaMainnetRPCURLs) == 0 {
    errs = append(errs, fmt.Errorf("SOLANA_MAINNET_RPC_URLS must contain at least one valid endpoint"))
}
```

Replace the devnet single URL loading:
```go
// OLD (lines 88-91):
cfg.SolanaDevnetRPCURL = os.Getenv("SOLANA_DEVNET_RPC_URL")
if cfg.SolanaDevnetRPCURL == "" {
    errs = append(errs, fmt.Errorf("SOLANA_DEVNET_RPC_URL is required"))
}

// NEW:
devnetURLsStr := os.Getenv("SOLANA_DEVNET_RPC_URLS")
if devnetURLsStr == "" {
    errs = append(errs, fmt.Errorf("SOLANA_DEVNET_RPC_URLS is required"))
}
cfg.SolanaDevnetRPCURLs = parseEndpoints(devnetURLsStr)
if len(cfg.SolanaDevnetRPCURLs) == 0 {
    errs = append(errs, fmt.Errorf("SOLANA_DEVNET_RPC_URLS must contain at least one valid endpoint"))
}
```

**Add helper function** (after the `Load()` function, around line 147):

```go
// parseEndpoints splits a comma-separated string of RPC endpoints and trims whitespace.
func parseEndpoints(endpointsStr string) []string {
    raw := strings.Split(endpointsStr, ",")
    endpoints := make([]string, 0, len(raw))
    for _, ep := range raw {
        trimmed := strings.TrimSpace(ep)
        if trimmed != "" {
            endpoints = append(endpoints, trimmed)
        }
    }
    return endpoints
}
```

**Remove the validation** that mainnet and devnet must be different (lines 99-101) since we now have lists.

### 5. Update RPC Client Initialization

**File:** `cmd/worker/main.go`

**Update mainnet client initialization (lines 85-87):**

```go
// OLD:
mainnetEndpoint := extractEndpointFromURL(cfg.SolanaMainnetRPCURL)
mainnetRPC := solana.NewRPCClient(cfg.SolanaMainnetRPCURL)
mainnetClient := solana.NewClient(mainnetRPC, mainnetEndpoint, metricsCollector, logger)

// NEW:
mainnetURL, err := solana.SelectRandomEndpoint(cfg.SolanaMainnetRPCURLs)
if err != nil {
    logger.Error("Failed to select mainnet RPC endpoint", "error", err)
    os.Exit(1)
}
mainnetEndpoint := extractEndpointFromURL(mainnetURL)
mainnetRPC := solana.NewRPCClient(mainnetURL)
mainnetClient := solana.NewClient(mainnetRPC, mainnetEndpoint, metricsCollector, logger)
logger.Info("Selected mainnet RPC endpoint", "endpoint", mainnetEndpoint, "url", mainnetURL)
```

**Update devnet client initialization (lines 94-96):**

```go
// OLD:
devnetEndpoint := extractEndpointFromURL(cfg.SolanaDevnetRPCURL)
devnetRPC := solana.NewRPCClient(cfg.SolanaDevnetRPCURL)
devnetClient := solana.NewClient(devnetRPC, devnetEndpoint, metricsCollector, logger)

// NEW:
devnetURL, err := solana.SelectRandomEndpoint(cfg.SolanaDevnetRPCURLs)
if err != nil {
    logger.Error("Failed to select devnet RPC endpoint", "error", err)
    os.Exit(1)
}
devnetEndpoint := extractEndpointFromURL(devnetURL)
devnetRPC := solana.NewRPCClient(devnetURL)
devnetClient := solana.NewClient(devnetRPC, devnetEndpoint, metricsCollector, logger)
logger.Info("Selected devnet RPC endpoint", "endpoint", devnetEndpoint, "url", devnetURL)
```

**Note:** The `extractEndpointFromURL()` function already exists in this file and handles provider identification for metrics.

### 6. Update Environment Examples

**Files:** `.env.worker.example` and `.env.server.example`

**Update Solana configuration section (lines 12-16):**

```bash
# Solana RPC Configuration
# Use comma-separated lists to distribute load across multiple endpoints
# Mix free public endpoints with paid services to avoid rate limits

# Mainnet endpoints - mix of free and paid for cost optimization
SOLANA_MAINNET_RPC_URLS=https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com,https://rpc.ankr.com/solana
USDC_MAINNET_MINT_ADDRESS=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v

# Devnet endpoints - typically free public endpoints are sufficient
SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com
USDC_DEVNET_MINT_ADDRESS=4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU
```

## Testing Strategy

### Unit Tests

**File:** `service/solana/adapter_test.go` (new file)

Test the endpoint selector:

```go
package solana

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSelectRandomEndpoint(t *testing.T) {
    t.Run("successful selection from multiple endpoints", func(t *testing.T) {
        endpoints := []string{
            "https://api.mainnet-beta.solana.com",
            "https://mainnet.helius-rpc.com",
            "https://rpc.ankr.com/solana",
        }

        selected, err := SelectRandomEndpoint(endpoints)
        require.NoError(t, err)
        assert.Contains(t, endpoints, selected)
    })

    t.Run("successful selection from single endpoint", func(t *testing.T) {
        endpoints := []string{"https://api.mainnet-beta.solana.com"}

        selected, err := SelectRandomEndpoint(endpoints)
        require.NoError(t, err)
        assert.Equal(t, endpoints[0], selected)
    })

    t.Run("error on empty slice", func(t *testing.T) {
        _, err := SelectRandomEndpoint([]string{})
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "no RPC endpoints configured")
    })

    t.Run("error on nil slice", func(t *testing.T) {
        _, err := SelectRandomEndpoint(nil)
        assert.Error(t, err)
    })

    t.Run("distribution across multiple calls", func(t *testing.T) {
        endpoints := []string{
            "https://endpoint1.com",
            "https://endpoint2.com",
            "https://endpoint3.com",
        }

        // Run 30 selections and verify we get different endpoints
        // (probabilistic test - very unlikely to select same endpoint 30 times)
        seen := make(map[string]bool)
        for i := 0; i < 30; i++ {
            selected, err := SelectRandomEndpoint(endpoints)
            require.NoError(t, err)
            seen[selected] = true
        }

        // With 30 selections from 3 endpoints, we should see at least 2 different ones
        assert.GreaterOrEqual(t, len(seen), 2, "Expected to see multiple endpoints selected")
    })
}
```

### Integration Tests

**File:** `service/config/config_test.go` (new file or add to existing)

Test configuration loading with multiple endpoints:

```go
package config

import (
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestConfigurationLoadingWithMultipleEndpoints(t *testing.T) {
    // Save original env vars to restore later
    originalMainnet := os.Getenv("SOLANA_MAINNET_RPC_URLS")
    originalDevnet := os.Getenv("SOLANA_DEVNET_RPC_URLS")
    defer func() {
        if originalMainnet != "" {
            os.Setenv("SOLANA_MAINNET_RPC_URLS", originalMainnet)
        } else {
            os.Unsetenv("SOLANA_MAINNET_RPC_URLS")
        }
        if originalDevnet != "" {
            os.Setenv("SOLANA_DEVNET_RPC_URLS", originalDevnet)
        } else {
            os.Unsetenv("SOLANA_DEVNET_RPC_URLS")
        }
    }()

    t.Run("parse multiple mainnet endpoints", func(t *testing.T) {
        os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com, https://mainnet.helius-rpc.com, https://rpc.ankr.com/solana")
        os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
        // Set other required env vars for full config loading
        // ...

        cfg, err := Load()
        require.NoError(t, err)

        assert.Len(t, cfg.SolanaMainnetRPCURLs, 3)
        assert.Equal(t, "https://api.mainnet-beta.solana.com", cfg.SolanaMainnetRPCURLs[0])
        assert.Equal(t, "https://mainnet.helius-rpc.com", cfg.SolanaMainnetRPCURLs[1])
        assert.Equal(t, "https://rpc.ankr.com/solana", cfg.SolanaMainnetRPCURLs[2])
    })

    t.Run("parse single devnet endpoint", func(t *testing.T) {
        os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
        os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
        // Set other required env vars for full config loading
        // ...

        cfg, err := Load()
        require.NoError(t, err)

        assert.Len(t, cfg.SolanaDevnetRPCURLs, 1)
        assert.Equal(t, "https://api.devnet.solana.com", cfg.SolanaDevnetRPCURLs[0])
    })

    t.Run("trim whitespace from endpoints", func(t *testing.T) {
        os.Setenv("SOLANA_MAINNET_RPC_URLS", "  https://a.com  ,  https://b.com  ,  https://c.com  ")
        os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
        // Set other required env vars for full config loading
        // ...

        cfg, err := Load()
        require.NoError(t, err)

        assert.Len(t, cfg.SolanaMainnetRPCURLs, 3)
        assert.Equal(t, "https://a.com", cfg.SolanaMainnetRPCURLs[0])
        assert.Equal(t, "https://b.com", cfg.SolanaMainnetRPCURLs[1])
        assert.Equal(t, "https://c.com", cfg.SolanaMainnetRPCURLs[2])
    })

    t.Run("error on empty mainnet endpoints", func(t *testing.T) {
        os.Setenv("SOLANA_MAINNET_RPC_URLS", "")
        os.Setenv("SOLANA_DEVNET_RPC_URLS", "https://api.devnet.solana.com")
        // Set other required env vars for full config loading
        // ...

        _, err := Load()
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "SOLANA_MAINNET_RPC_URLS")
    })

    t.Run("error on empty devnet endpoints", func(t *testing.T) {
        os.Setenv("SOLANA_MAINNET_RPC_URLS", "https://api.mainnet-beta.solana.com")
        os.Setenv("SOLANA_DEVNET_RPC_URLS", "")
        // Set other required env vars for full config loading
        // ...

        _, err := Load()
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "SOLANA_DEVNET_RPC_URLS")
    })
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
```

### Manual Testing

After deployment, verify endpoint distribution:

```bash
# Check worker logs to see which endpoints are being selected
journalctl -u forohtoo-worker -f | grep "Selected.*RPC endpoint"

# Or if using file logging:
tail -f /var/log/forohtoo/worker.log | jq 'select(.msg | contains("Selected")) | {time: .time, msg: .msg, endpoint: .endpoint, url: .url}'
```

## Expected Benefits

1. **Rate Limit Mitigation**: Distribute queries across multiple providers
2. **Cost Optimization**: Mix free public endpoints with paid services
3. **Improved Reliability**: If one provider is slow/down, others handle load
4. **Flexibility**: Easy to add/remove providers by updating env var
5. **No Vendor Lock-in**: Can switch providers without code changes

## Recommended Endpoint Mix for Forohtoo

### Mainnet Endpoints

For production mainnet, mix free and paid services to optimize cost while maintaining reliability:

**Recommended configuration (with authentication):**
```bash
SOLANA_MAINNET_RPC_URLS=https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY,https://rpc.ankr.com/solana,https://solana.rpcpool.com
```

**Endpoint breakdown:**

1. **Free Public RPCs** (for basic transaction queries):
   - `https://api.mainnet-beta.solana.com` - Solana Foundation (official)
   - `https://rpc.ankr.com/solana` - Ankr (good uptime)
   - `https://solana.rpcpool.com` - RPCPool (reliable community provider)
   - **Note:** Free endpoints have lower rate limits but are sufficient for many operations

2. **Paid Services** (for high-volume, time-sensitive operations):
   - `https://mainnet.helius-rpc.com` - Your current Helius endpoint (keep your API key in URL or use API_KEY param)
   - Consider adding: Alchemy, QuickNode, Triton One if needed for higher throughput

**Cost optimization strategy:**
- Random selection distributes ~25% of load to each endpoint (with 4 total)
- If Helius is 1 of 4 endpoints, you reduce Helius costs by ~75%
- Free endpoints absorb majority of load, paid endpoint handles remainder
- If a free endpoint is rate-limited or down, others compensate automatically

### Devnet Endpoints

For development/testing, free public endpoints are typically sufficient:

```bash
SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com
```

Can add more if needed:
```bash
SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com,https://devnet.helius-rpc.com
```

### Migration Strategy (Direct Cutover)

**Simple direct migration - no gradual rollout needed:**

1. **Implement all code changes** (follow the migration checklist)
   - Update config struct
   - Add `SelectRandomEndpoint()` and `parseEndpoints()` functions
   - Update configuration loading
   - Update RPC client initialization
   - Add tests

2. **Update environment variables** with multiple endpoints immediately:
   ```bash
   # Replace old single-endpoint vars:
   # SOLANA_MAINNET_RPC_URL=https://mainnet.helius-rpc.com/?api-key=YOUR_KEY
   # SOLANA_DEVNET_RPC_URL=https://api.devnet.solana.com

   # New multi-endpoint vars (mix of free + paid):
   SOLANA_MAINNET_RPC_URLS=https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY,https://rpc.ankr.com/solana,https://solana.rpcpool.com
   SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com
   ```

3. **Deploy and restart worker**
   - Build new worker binary
   - Deploy to production
   - Restart worker service

4. **Monitor immediately after deployment:**
   ```bash
   # Watch for endpoint selection in logs
   journalctl -u forohtoo-worker -f | grep "Selected.*RPC endpoint"

   # Or with jq if using file logging:
   tail -f /var/log/forohtoo/worker.log | jq 'select(.msg | contains("Selected"))'
   ```

5. **Verify success:**
   - ✅ Worker starts without errors
   - ✅ Logs show random endpoint selection (different endpoints chosen)
   - ✅ Check Helius dashboard - should see ~75% reduction in API calls (if using 4 endpoints with 1 being Helius)
   - ✅ Monitor Prometheus metrics - verify rate limit hits decrease
   - ✅ Verify transactions still processing correctly

**Rollback plan (if needed):**

If issues arise, revert environment variables and restart:
```bash
# Revert to single endpoint
SOLANA_MAINNET_RPC_URLS=https://mainnet.helius-rpc.com/?api-key=YOUR_HELIUS_KEY
SOLANA_DEVNET_RPC_URLS=https://api.devnet.solana.com

# Restart worker
sudo systemctl restart forohtoo-worker
```

**Expected immediate benefits:**
- ~75% cost reduction on Helius (with 4 endpoints where 1 is Helius)
- Improved resilience (if one endpoint is slow/down, others handle load)
- Reduced rate limiting issues

## Future Enhancements (Not in Initial Implementation)

These can be added later if needed:

1. **Health Checking**: Periodically test endpoints, remove unhealthy ones from pool
2. **Weighted Selection**: Prefer certain endpoints (e.g., 70% paid, 30% free)
3. **Retry with Different Endpoint**: On failure, retry with different endpoint
4. **Metrics/Logging**: Track which endpoints are used, error rates per endpoint
5. **Circuit Breaker**: Temporarily remove failing endpoints from pool

## Migration Checklist for Forohtoo Project

### Code Changes

- [ ] **Add `SelectRandomEndpoint()` function**
  - File: `service/solana/adapter.go` (around line 18, before `NewRPCClient()`)
  - Add `math/rand` import if not already present

- [ ] **Update Config struct**
  - File: `service/config/config.go`
  - Line 25: Change `SolanaMainnetRPCURL string` → `SolanaMainnetRPCURLs []string`
  - Line 29: Change `SolanaDevnetRPCURL string` → `SolanaDevnetRPCURLs []string`

- [ ] **Add `parseEndpoints()` helper function**
  - File: `service/config/config.go` (after `Load()` function, around line 147)

- [ ] **Update configuration loading in `Load()` function**
  - File: `service/config/config.go`
  - Lines 77-80: Update mainnet URL loading to parse `SOLANA_MAINNET_RPC_URLS`
  - Lines 88-91: Update devnet URL loading to parse `SOLANA_DEVNET_RPC_URLS`
  - Lines 99-101: Remove validation that mainnet and devnet must be different (no longer relevant)

- [ ] **Update mainnet RPC client initialization**
  - File: `cmd/worker/main.go` (lines 85-87)
  - Call `solana.SelectRandomEndpoint(cfg.SolanaMainnetRPCURLs)`
  - Add error handling for endpoint selection
  - Add log message showing selected endpoint

- [ ] **Update devnet RPC client initialization**
  - File: `cmd/worker/main.go` (lines 94-96)
  - Call `solana.SelectRandomEndpoint(cfg.SolanaDevnetRPCURLs)`
  - Add error handling for endpoint selection
  - Add log message showing selected endpoint

### Environment Files

- [ ] **Update worker environment example**
  - File: `.env.worker.example` (lines 12-16)
  - Change `SOLANA_MAINNET_RPC_URL` → `SOLANA_MAINNET_RPC_URLS`
  - Change `SOLANA_DEVNET_RPC_URL` → `SOLANA_DEVNET_RPC_URLS`
  - Add example with multiple comma-separated endpoints
  - Add explanatory comments about load distribution

- [ ] **Update server environment example**
  - File: `.env.server.example` (lines 12-16)
  - Same changes as worker example

### Testing

- [ ] **Add unit tests for endpoint selection**
  - Create file: `service/solana/adapter_test.go`
  - Test successful selection from multiple endpoints
  - Test successful selection from single endpoint
  - Test error on empty/nil slice
  - Test distribution across multiple calls

- [ ] **Add configuration loading tests**
  - File: `service/config/config_test.go` (create if doesn't exist)
  - Test parsing multiple endpoints
  - Test trimming whitespace
  - Test error on empty endpoints
  - Test `parseEndpoints()` function

- [ ] **Run existing tests to verify no regressions**
  - Run: `make test` or `go test ./...`
  - Ensure all existing tests pass with new configuration

### Deployment

- [ ] **Update production environment variables**
  - Change `SOLANA_MAINNET_RPC_URL` → `SOLANA_MAINNET_RPC_URLS` with comma-separated list
  - Change `SOLANA_DEVNET_RPC_URL` → `SOLANA_DEVNET_RPC_URLS` with comma-separated list
  - Include mix of free and paid endpoints for mainnet
  - Initially can use single endpoint to maintain current behavior

- [ ] **Deploy updated code**
  - Build new worker binary with changes
  - Deploy to production environment
  - Restart worker service

- [ ] **Monitor logs for endpoint selection**
  - Check logs for "Selected mainnet RPC endpoint" messages
  - Check logs for "Selected devnet RPC endpoint" messages
  - Verify random distribution across multiple endpoints
  - Monitor for any errors in endpoint selection

- [ ] **Monitor RPC metrics**
  - Check Prometheus metrics for RPC call distribution by endpoint
  - Verify rate limit hits are reduced (fewer `rate_limit_hit` metrics)
  - Verify no increase in errors after migration

### Documentation

- [ ] **Update README or deployment docs**
  - Document new environment variable format
  - Explain multi-endpoint strategy
  - Provide examples of recommended endpoint combinations

- [ ] **Update CHANGELOG.md**
  - Add entry for multi-endpoint RPC support
  - Note breaking change in environment variable names

### Optional Future Enhancements

- [ ] Add health checking for endpoints (periodically test and remove unhealthy ones)
- [ ] Add weighted selection (prefer paid endpoints over free ones)
- [ ] Add retry with different endpoint on failure
- [ ] Add per-endpoint metrics tracking (success rate, latency)
- [ ] Add circuit breaker to temporarily disable failing endpoints

## Important Notes

### API Key Management Across Providers

**Good news:** All major Solana RPC providers embed authentication directly in the URL, making multi-endpoint management straightforward. No separate header-based authentication or complex SDK setup required.

#### Provider Authentication Formats

| Provider | Authentication Format | Example URL |
|----------|----------------------|-------------|
| **Solana Foundation** | None (public) | `https://api.mainnet-beta.solana.com` |
| **Helius** | Query parameter | `https://mainnet.helius-rpc.com/?api-key=YOUR_KEY` |
| **Alchemy** | Path segment | `https://solana-mainnet.g.alchemy.com/v2/YOUR_API_KEY` |
| **QuickNode** | Path segment | `https://your-endpoint.solana-mainnet.quiknode.pro/YOUR_TOKEN/` |
| **Ankr (Free)** | None (public) | `https://rpc.ankr.com/solana` |
| **Ankr (Premium)** | Path segment (JWT) | `https://rpc.ankr.com/solana/YOUR_TOKEN` |
| **Triton One / RPCPool** | Path segment | `https://your-app.mainnet.rpcpool.com/YOUR_TOKEN` |

#### Implementation Approach

**Recommended: Include full authenticated URLs in environment variable**

Since all providers use URL-based authentication, simply include the complete, authenticated URL for each endpoint:

```bash
SOLANA_MAINNET_RPC_URLS=https://api.mainnet-beta.solana.com,https://mainnet.helius-rpc.com/?api-key=abc123,https://solana-mainnet.g.alchemy.com/v2/xyz789,https://rpc.ankr.com/solana
```

**Benefits of this approach:**
- ✅ Works with all providers out of the box
- ✅ No code changes needed to support different providers
- ✅ Easy to add/remove/rotate endpoints
- ✅ Each endpoint is self-contained with its authentication
- ✅ No complex SDK or header management needed

**Example production configuration:**

```bash
# Mix of free public endpoints and paid services
SOLANA_MAINNET_RPC_URLS=\
https://api.mainnet-beta.solana.com,\
https://mainnet.helius-rpc.com/?api-key=your-helius-key-here,\
https://solana-mainnet.g.alchemy.com/v2/your-alchemy-key-here,\
https://rpc.ankr.com/solana,\
https://your-endpoint.mainnet.rpcpool.com/your-triton-token
```

#### Security Considerations

**Is it safe to put API keys in the URL?**

Yes, when stored in environment variables:
- Environment variables are already considered secret configuration
- API keys in URLs are **no less secure** than separate `API_KEY` env vars
- The URL string is only stored in your secure environment, not in code or version control

**Best practices:**
- ✅ Never commit `.env` files to version control (already in your `.gitignore`)
- ✅ Use secure secrets management in production (Vault, AWS Secrets Manager, etc.)
- ✅ Rotate API keys periodically
- ✅ Use provider dashboards to restrict access by IP or domain if available
- ✅ Monitor usage to detect unauthorized access
- ❌ Never log the full URL (your logging already extracts endpoint names, which is good)

**What about URL encoding?**

Most API keys are alphanumeric and don't need encoding. If your API key contains special characters (rare), URL-encode them:
- Spaces: `%20`
- Special chars: use standard URL encoding

The Go HTTP client handles this automatically when constructing requests.

### Other Implementation Notes

- **No WebSocket Changes**: This strategy applies only to HTTP RPC endpoints, not WebSocket connections
- **No Backward Compatibility Needed**: This is a breaking change (env var names change), but migration is straightforward
- **No Health Checking**: Initial implementation doesn't need health checks on startup - can add later if needed
- **Go 1.20+ Random**: Modern Go versions automatically seed `rand`, no manual seeding needed
- **Temporal Workers**: Since workers are stateless and restart frequently, endpoint selection happens at worker startup, ensuring distribution across workers
- **Metrics Impact**: The `extractEndpointFromURL()` function already handles identifying providers for metrics, so endpoint-specific metrics will work automatically

## References

- Solana RPC Endpoints: https://docs.solana.com/cluster/rpc-endpoints
- Helius: https://www.helius.dev/
- Alchemy: https://www.alchemy.com/solana
- QuickNode: https://www.quicknode.com/chains/sol
