-- Rollback: Remove network column from transactions table

-- Drop new indexes
DROP INDEX IF EXISTS idx_transactions_signature_network;
DROP INDEX IF EXISTS idx_transactions_wallet_network_time;
DROP INDEX IF EXISTS idx_transactions_wallet_network;

-- Recreate original indexes
CREATE UNIQUE INDEX idx_transactions_signature ON transactions(signature, block_time);
CREATE INDEX idx_transactions_wallet_address ON transactions(wallet_address, block_time DESC);
CREATE INDEX idx_transactions_wallet_time ON transactions(wallet_address, block_time DESC);

-- Remove network column
ALTER TABLE transactions
DROP COLUMN network;
