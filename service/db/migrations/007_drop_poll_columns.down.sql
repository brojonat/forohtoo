-- Restore polling-related columns and indexes.
ALTER TABLE wallets ADD COLUMN poll_interval INTERVAL NOT NULL DEFAULT '30 seconds';
ALTER TABLE wallets ADD COLUMN last_poll_time TIMESTAMPTZ;

CREATE INDEX idx_wallets_network_poll_queue
    ON wallets (network, asset_type, token_mint, last_poll_time ASC)
    WHERE status = 'active';
