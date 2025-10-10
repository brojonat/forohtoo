-- Create transactions table
CREATE TABLE transactions (
    signature VARCHAR(88) NOT NULL,  -- Solana transaction signature (base58)
    wallet_address VARCHAR(44) NOT NULL,  -- Solana wallet address (base58)
    slot BIGINT NOT NULL,  -- Solana slot number
    block_time TIMESTAMPTZ NOT NULL,  -- When transaction was confirmed on-chain
    amount BIGINT NOT NULL,  -- Amount in lamports (for SOL) or smallest token unit
    token_mint VARCHAR(44),  -- SPL token mint address, NULL for native SOL
    memo TEXT,  -- Transaction memo (often JSON for workflow_id matching)
    confirmation_status VARCHAR(20) NOT NULL DEFAULT 'finalized',  -- finalized, confirmed, etc
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),  -- When we inserted this record
    PRIMARY KEY (signature, block_time)  -- Composite PK required for TimescaleDB partitioning
);

-- Unique constraint on signature alone (since signatures are globally unique)
CREATE UNIQUE INDEX idx_transactions_signature ON transactions(signature, block_time);

-- Index for querying transactions by wallet
CREATE INDEX idx_transactions_wallet_address ON transactions(wallet_address, block_time DESC);

-- Index for querying transactions by block_time (important for time-series queries)
CREATE INDEX idx_transactions_block_time ON transactions(block_time DESC);

-- Composite index for wallet + time range queries (common pattern)
CREATE INDEX idx_transactions_wallet_time ON transactions(wallet_address, block_time DESC);

-- Convert to TimescaleDB hypertable for efficient time-series queries
-- This requires TimescaleDB extension to be enabled
SELECT create_hypertable('transactions', 'block_time', if_not_exists => TRUE);

-- Optional: Create a retention policy (e.g., keep data for 2 years)
-- Uncomment if you want automatic data deletion
-- SELECT add_retention_policy('transactions', INTERVAL '2 years');
