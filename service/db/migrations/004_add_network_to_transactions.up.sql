-- Add network column to transactions table
-- This enables support for multiple Solana networks (mainnet, devnet, testnet)

-- Add network column with default value for existing rows
ALTER TABLE transactions
ADD COLUMN network VARCHAR(20) NOT NULL DEFAULT 'mainnet';

-- Remove the default after backfilling (keeps the NOT NULL constraint)
ALTER TABLE transactions
ALTER COLUMN network DROP DEFAULT;

-- Drop existing indexes that don't include network
DROP INDEX IF EXISTS idx_transactions_signature;
DROP INDEX IF EXISTS idx_transactions_wallet_address;
DROP INDEX IF EXISTS idx_transactions_wallet_time;

-- Recreate indexes with network for proper isolation
-- Unique constraint on (signature, network, block_time)
-- Signatures are unique per network, not globally
CREATE UNIQUE INDEX idx_transactions_signature_network ON transactions(signature, network, block_time);

-- Index for querying transactions by wallet and network
CREATE INDEX idx_transactions_wallet_network_time ON transactions(wallet_address, network, block_time DESC);

-- Composite index for wallet + network + time range queries (common pattern)
CREATE INDEX idx_transactions_wallet_network ON transactions(wallet_address, network);

-- Add comment explaining the network field
COMMENT ON COLUMN transactions.network IS 'Solana network where transaction occurred (mainnet, devnet, testnet)';
