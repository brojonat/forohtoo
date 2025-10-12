# Client Await Command Examples

The `forohtoo wallet await` command blocks until a transaction matching specific criteria arrives via SSE. It's designed for payment gating and uses a closure-based approach to combine multiple filters with AND logic.

## Available Filters

- `--signature`: Exact transaction signature match
- `--usdc-amount-equal`: Exact USDC amount (e.g., 0.42 for 0.42 USDC)
- `--must-jq`: jq filter expression that must evaluate to true (can be specified multiple times)

## Basic Usage

### Wait for a specific signature

```bash
forohtoo wallet await --signature "5Kn8VF..." \
  YOUR_WALLET_ADDRESS
```

## USDC Amount Filtering

### Wait for exact USDC amount

```bash
forohtoo wallet await --usdc-amount-equal 0.42 \
  YOUR_WALLET_ADDRESS
```

This checks that the transaction amount equals exactly 0.42 USDC (420000 lamports, since USDC has 6 decimals).

## jq Filter Expressions

The `--must-jq` flag accepts jq expressions that run against the transaction memo (parsed as JSON). Multiple filters can be specified, and **all must evaluate to true** for the transaction to match.

### Simple field match

```bash
forohtoo wallet await --must-jq '.workflow_id == "test-123"' \
  YOUR_WALLET_ADDRESS
```

### Contains check

```bash
forohtoo wallet await --must-jq '. | contains({workflow_id: "test-123"})' \
  YOUR_WALLET_ADDRESS
```

### Nested field access

```bash
forohtoo wallet await --must-jq '.metadata.order_id == "12345"' \
  YOUR_WALLET_ADDRESS
```

### Complex boolean logic

```bash
forohtoo wallet await --must-jq '.amount_usd > 0 and .amount_usd <= 100' \
  YOUR_WALLET_ADDRESS
```

## Combining Multiple Filters

All filters are combined with AND logic. The transaction must match **all** criteria:

### Example: USDC amount + workflow_id

```bash
forohtoo wallet await --usdc-amount-equal 0.42 \
  --must-jq '. | contains({workflow_id: "some-id"})' \
  DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK
```

This matches only if:
1. Transaction amount is exactly 0.42 USDC (420000 lamports)
2. Memo (parsed as JSON) contains a `workflow_id` field equal to "some-id"

### Example: Multiple jq filters

```bash
forohtoo wallet await --must-jq '.workflow_id == "test-123"' \
  --must-jq '.metadata.order_id == "12345"' \
  --must-jq '.amount_usd >= 0.42' \
  YOUR_WALLET_ADDRESS
```

This matches only if:
1. Memo has `workflow_id` equal to "test-123"
2. Memo has `metadata.order_id` equal to "12345"
3. Memo has `amount_usd` >= 0.42

### Example: All filters combined

```bash
forohtoo wallet await --usdc-amount-equal 1.0 \
  --must-jq '. | contains({workflow_id: "payment-workflow-123"})' \
  --must-jq '.metadata.user_id == "user-456"' \
  --must-jq '.metadata.tier == "premium"' \
  YOUR_WALLET_ADDRESS
```

This matches only if:
1. Transaction amount is exactly 1.0 USDC
2. Memo contains `workflow_id` field equal to "payment-workflow-123"
3. Memo has `metadata.user_id` equal to "user-456"
4. Memo has `metadata.tier` equal to "premium"

## Options

### Timeout

Default timeout is 5 minutes. You can adjust it:

```bash
forohtoo wallet await --must-jq '. | contains({workflow_id: "test-123"})' \
  --timeout 10m \
  YOUR_WALLET_ADDRESS
```

### JSON Output

For automation/scripting, use `--json` to output the matching transaction as JSON:

```bash
forohtoo wallet await --must-jq '. | contains({workflow_id: "test-123"})' \
  --json \
  YOUR_WALLET_ADDRESS
```

### Custom Server URL

```bash
forohtoo wallet await --must-jq '. | contains({workflow_id: "test-123"})' \
  --server https://production-server.com:8080 \
  YOUR_WALLET_ADDRESS
```

Or use the environment variable:

```bash
export FOROHTOO_SERVER_URL=https://production-server.com:8080
forohtoo wallet await --must-jq '. | contains({workflow_id: "test-123"})' \
  YOUR_WALLET_ADDRESS
```

## How It Works

The CLI builds a matcher function (closure) that combines all your filters (this is not a bad approach if you are leveraging the client yourself!):

```go
matcher := func(txn *client.Transaction) bool {
    // 1. Check signature (if specified)
    if signature != "" && txn.Signature != signature {
        return false
    }

    // 2. Check USDC amount (if specified)
    if usdcAmount != 0 {
        expectedLamports := int64(usdcAmount * 1e6)
        if txn.Amount != expectedLamports {
            return false
        }
    }

    // 3. Check all jq filters (if specified)
    // Parse memo as JSON and run each jq filter
    // ALL must evaluate to true

    return true // All conditions passed
}
```

This matcher is passed to `client.Await()`, which:
1. Connects to the SSE stream for the specified wallet
2. Receives each transaction event
3. Calls the matcher function
4. Returns the first transaction where the matcher returns `true`

## Error Handling

If the memo cannot be parsed as JSON and jq filters are specified, the transaction will not match (filters fail gracefully).

If a jq filter has a syntax error, the command will fail immediately with a parse error.

If the context timeout is reached before a match, the command returns an error.

## Example: IncentivizeThis Workflow

For the IncentivizeThis use case described in the README:

1. Create a bounty with workflow_id "bounty-abc123" and amount 0.42 USDC
2. Run await command:

```bash
forohtoo wallet await --usdc-amount-equal 0.42 \
  --must-jq '. | contains({workflow_id: "bounty-abc123"})' \
  DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK
```

3. User scans QR code and sends 0.42 USDC with memo: `{"workflow_id": "bounty-abc123", "bounty_title": "Fix bug #42"}`
4. Command unblocks and prints transaction details
5. Workflow continues with order fulfillment
