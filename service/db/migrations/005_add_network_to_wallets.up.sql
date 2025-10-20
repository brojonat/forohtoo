-- Add network column to wallets table
ALTER TABLE wallets
ADD COLUMN network VARCHAR(20) NOT NULL DEFAULT 'mainnet';

-- Drop old primary key
ALTER TABLE wallets DROP CONSTRAINT wallets_pkey;

-- Create new composite primary key (address, network)
-- This allows same address on different networks
ALTER TABLE wallets
ADD PRIMARY KEY (address, network);

-- Drop old indexes
DROP INDEX IF EXISTS idx_wallets_status_updated;
DROP INDEX IF EXISTS idx_wallets_poll_queue;

-- Recreate indexes with network awareness
CREATE INDEX idx_wallets_network_status_updated ON wallets(network, status, updated_at DESC);
CREATE INDEX idx_wallets_network_poll_queue ON wallets(network, last_poll_time ASC) WHERE status = 'active';

-- Remove default after data migration is complete
ALTER TABLE wallets ALTER COLUMN network DROP DEFAULT;
