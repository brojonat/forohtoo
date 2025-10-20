-- Recreate old indexes
CREATE INDEX idx_wallets_status_updated ON wallets(status, updated_at DESC);
CREATE INDEX idx_wallets_poll_queue ON wallets(last_poll_time ASC) WHERE status = 'active';

-- Drop network-aware indexes
DROP INDEX IF EXISTS idx_wallets_network_status_updated;
DROP INDEX IF EXISTS idx_wallets_network_poll_queue;

-- Drop composite primary key
ALTER TABLE wallets DROP CONSTRAINT wallets_pkey;

-- Recreate single-column primary key
ALTER TABLE wallets ADD PRIMARY KEY (address);

-- Remove network column
ALTER TABLE wallets DROP COLUMN network;
