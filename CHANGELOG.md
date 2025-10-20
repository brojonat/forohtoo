# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Multi-network support**: Service now supports monitoring wallets on both Solana mainnet and devnet simultaneously
  - New `network` parameter (mainnet|devnet) required for all wallet operations
  - Separate RPC endpoints and USDC mint addresses for each network
  - Database schema updated to store network with transactions and wallets
  - Dual RPC client architecture with independent rate limiting per network
  - Same wallet address can be monitored on multiple networks
- CLI `--network` flag added to all wallet commands (defaults to "mainnet" for backward compatibility)
- Network-aware Temporal workflow scheduling with separate schedules per network

### Changed
- **BREAKING**: Configuration now requires both `SOLANA_MAINNET_RPC_URL` and `SOLANA_DEVNET_RPC_URL` (replaces `SOLANA_RPC_URL`)
- **BREAKING**: Configuration now requires both `USDC_MAINNET_MINT_ADDRESS` and `USDC_DEVNET_MINT_ADDRESS`
- **BREAKING**: API endpoints now require `network` parameter:
  - `POST /api/v1/wallets` - network in JSON body
  - `GET /api/v1/wallets/{address}?network={network}` - network as query parameter
  - `DELETE /api/v1/wallets/{address}?network={network}` - network as query parameter
  - `GET /api/v1/stream/transactions/{address}?network={network}` - network as query parameter
  - `GET /api/v1/transactions?wallet_address={address}&network={network}` - network as query parameter
- Database schema: `wallets` table now has composite primary key (address, network)
- Database schema: `transactions` table now includes NOT NULL `network` column
- Client library: All methods now require `network` parameter

### Migration Guide
- Update environment variables:
  - Rename `SOLANA_RPC_URL` to `SOLANA_MAINNET_RPC_URL`
  - Add `SOLANA_DEVNET_RPC_URL`
  - Add `USDC_MAINNET_MINT_ADDRESS=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v`
  - Add `USDC_DEVNET_MINT_ADDRESS=4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU`
- Run database migrations: `migrate -path service/db/migrations -database $DATABASE_URL up`
- Update API calls to include `network` parameter
- Update CLI commands to include `--network` flag (or rely on default "mainnet")
