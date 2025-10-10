-- Create wallets table for tracking which wallets to monitor
CREATE TABLE wallets (
    address VARCHAR(44) PRIMARY KEY,
    poll_interval INTERVAL NOT NULL,
    last_poll_time TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for listing wallets by status and update time
CREATE INDEX idx_wallets_status_updated ON wallets(status, updated_at DESC);

-- Index for finding wallets that need polling (active wallets, ordered by last poll time)
CREATE INDEX idx_wallets_poll_queue ON wallets(last_poll_time ASC) WHERE status = 'active';
