-- Add asset columns to wallets table
-- This enables mint-driven polling where each wallet+asset combination is monitored separately

-- Add asset_type column (sol or spl-token)
ALTER TABLE wallets
ADD COLUMN asset_type VARCHAR(20) NOT NULL DEFAULT 'spl-token';

-- Add token_mint column (empty string for SOL, mint address for SPL tokens)
-- NOT NULL to simplify primary key management
ALTER TABLE wallets
ADD COLUMN token_mint VARCHAR(44) NOT NULL DEFAULT '';

-- Add associated_token_address column (NULL for SOL, ATA for SPL tokens)
ALTER TABLE wallets
ADD COLUMN associated_token_address VARCHAR(44);

-- Set default values for existing wallets based on USDC migration strategy
-- All existing wallets are assumed to be USDC monitoring
-- Token mints will be set via data migration script after this schema migration

-- Drop old primary key
ALTER TABLE wallets DROP CONSTRAINT wallets_pkey;

-- Create new composite primary key (address, network, asset_type, token_mint)
-- This allows same wallet to monitor multiple assets (SOL, USDC, other tokens)
ALTER TABLE wallets
ADD PRIMARY KEY (address, network, asset_type, token_mint);

-- Drop old indexes
DROP INDEX IF EXISTS idx_wallets_network_status_updated;
DROP INDEX IF EXISTS idx_wallets_network_poll_queue;

-- Recreate indexes with asset awareness
-- Index for listing wallets by network, status, and update time
CREATE INDEX idx_wallets_network_status_updated ON wallets(network, status, updated_at DESC);

-- Index for finding wallets that need polling (active wallets, ordered by last poll time)
-- Includes asset_type and token_mint to support asset-specific queries
CREATE INDEX idx_wallets_network_poll_queue ON wallets(network, asset_type, token_mint, last_poll_time ASC) WHERE status = 'active';

-- Index for querying by address across all assets
CREATE INDEX idx_wallets_address ON wallets(address);

-- Remove defaults after migration is complete
ALTER TABLE wallets ALTER COLUMN asset_type DROP DEFAULT;
ALTER TABLE wallets ALTER COLUMN token_mint DROP DEFAULT;
