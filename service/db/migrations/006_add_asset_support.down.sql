-- Revert asset support changes to wallets table

-- Drop new indexes
DROP INDEX IF EXISTS idx_wallets_address;
DROP INDEX IF EXISTS idx_wallets_network_poll_queue;
DROP INDEX IF EXISTS idx_wallets_network_status_updated;

-- Drop new composite primary key
ALTER TABLE wallets DROP CONSTRAINT wallets_pkey;

-- Remove asset columns
ALTER TABLE wallets DROP COLUMN IF EXISTS associated_token_address;
ALTER TABLE wallets DROP COLUMN IF EXISTS token_mint;
ALTER TABLE wallets DROP COLUMN IF EXISTS asset_type;

-- Restore old primary key (address, network)
ALTER TABLE wallets
ADD PRIMARY KEY (address, network);

-- Recreate old indexes
CREATE INDEX idx_wallets_network_status_updated ON wallets(network, status, updated_at DESC);
CREATE INDEX idx_wallets_network_poll_queue ON wallets(network, last_poll_time ASC) WHERE status = 'active';
