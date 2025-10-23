# Migration Guide: Asset-Aware Wallet Polling

This guide explains how to migrate from the legacy dual-polling system to the new mint-driven, asset-aware polling system.

## Overview

The new system allows wallets to register specific assets (SOL or SPL tokens) for monitoring, with each asset getting its own Temporal schedule. This eliminates unnecessary RPC calls and provides explicit control over what's being monitored.

## Migration Steps

### 1. Run Database Schema Migration

Apply the schema migration to add asset columns to the wallets table:

```bash
# Using your migration tool (e.g., golang-migrate)
migrate -path service/db/migrations -database "$DATABASE_URL" up
```

This adds:
- `asset_type` column (sol or spl-token)
- `token_mint` column (empty for SOL, mint address for SPL tokens)
- `associated_token_address` column (NULL for SOL, ATA for SPL tokens)

And updates the primary key to: `(address, network, asset_type, token_mint)`

### 2. Run Data Migration

After the schema migration, populate asset data for existing wallets:

```bash
# Build the migration tool
go build -o bin/migrate-assets ./cmd/migrate-assets

# Set required environment variables (same as your worker/server)
export DATABASE_URL="postgres://..."
export USDC_MAINNET_MINT_ADDRESS="EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
export USDC_DEVNET_MINT_ADDRESS="4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"

# Run the migration
./bin/migrate-assets
```

This script:
- Reads all existing wallets
- Computes the USDC ATA for each wallet based on network
- Updates each row with `token_mint` and `associated_token_address`

### 3. Update Temporal Schedules

After data migration, existing Temporal schedules need to be recreated with asset-aware IDs:

**Option A: Let them recreate naturally**
- Delete all existing schedules: `forohtoo wallet list | jq -r '.[] | "\(.address) \(.network)"' | xargs -n2 forohtoo wallet remove`
- Re-register wallets using the new API: `forohtoo wallet add <address> --asset-mint <mint>`

**Option B: Manual schedule migration**
- Use Temporal UI or CLI to delete old schedules
- New registrations will create asset-scoped schedules automatically

### 4. Deploy Updated Services

Deploy the updated worker and server with the new asset-aware code:

```bash
# Stop existing services
systemctl stop forohtoo-worker
systemctl stop forohtoo-server

# Deploy new binaries
go build -o bin/worker ./cmd/worker
go build -o bin/server ./cmd/server

# Start services
systemctl start forohtoo-worker
systemctl start forohtoo-server
```

### 5. Verify Migration

Check that wallets are now polling with asset information:

```bash
# List all registered wallet-assets
forohtoo wallet list

# Verify each shows:
# - asset_type: "spl-token"
# - token_mint: USDC mint address
# - associated_token_address: computed ATA
```

Check Temporal schedules have the new format:
- Old: `poll-wallet-mainnet-<address>`
- New: `poll-wallet-mainnet-<address>-spl-token-<mint>`

### 6. Test New Asset Registrations

Test the new registration flow:

```bash
# Register a wallet for SOL monitoring
forohtoo wallet add <address> --network mainnet --asset sol

# Register a wallet for USDC monitoring
forohtoo wallet add <address> --network mainnet --asset-mint EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v

# Same wallet can now have multiple assets
forohtoo wallet list | jq '.[] | select(.address == "<address>")'
```

## Rollback Procedure

If issues arise, you can rollback:

```bash
# Run down migration
migrate -path service/db/migrations -database "$DATABASE_URL" down 1

# Revert to previous service binaries
git checkout <previous-version>
go build -o bin/worker ./cmd/worker
go build -o bin/server ./cmd/server
systemctl restart forohtoo-worker forohtoo-server
```

## Important Notes

1. **Breaking Change**: The API and CLI have changed. Update any external clients or scripts.
2. **Schedule IDs**: Old schedule IDs won't match new format. Clean up manually if needed.
3. **Migration Strategy**: This guide assumes USDC-only migration. Adjust if you need SOL support immediately.
4. **Temporal Schedules**: Consider the total schedule count impact if you have many wallets.

## Troubleshooting

### Migration script fails with "column already exists"

The schema migration already ran. Skip step 1 and proceed to step 2.

### Data migration fails on some wallets

Check logs for specific errors. Common issues:
- Invalid wallet address format
- Unknown network value
- Missing USDC mint configuration

### Schedules still showing old format

Delete old schedules manually using Temporal UI or CLI, then re-register wallets.

### RPC rate limiting still occurring

Verify workflows are only polling one address per run (no 2-second delay in logs).
Check Prometheus metrics for `asset_type` and `token_mint` labels.

## Post-Migration Validation

1. **Database**: `SELECT address, network, asset_type, token_mint, associated_token_address FROM wallets LIMIT 10;`
2. **Metrics**: Check Prometheus for `forohtoo_poll_` metrics with asset labels
3. **Logs**: Search for "polling ATA" or "polling wallet" to verify single address per run
4. **Temporal**: Verify schedule IDs include asset information
5. **API**: Test new endpoints return asset data in responses
