-- Drop polling-related state from wallets. Transaction ingestion is now
-- handled by Helius webhooks; there is no per-wallet poll cadence to track.
DROP INDEX IF EXISTS idx_wallets_network_poll_queue;
DROP INDEX IF EXISTS idx_wallets_poll_queue;

ALTER TABLE wallets DROP COLUMN IF EXISTS poll_interval;
ALTER TABLE wallets DROP COLUMN IF EXISTS last_poll_time;
