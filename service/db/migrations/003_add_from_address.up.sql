-- Add from_address column to track the sender of the transaction
ALTER TABLE transactions
ADD COLUMN from_address VARCHAR(44);

-- Add index for querying by sender
CREATE INDEX idx_transactions_from_address ON transactions(from_address, block_time DESC);

-- Add comment for clarity
COMMENT ON COLUMN transactions.from_address IS 'Source wallet address (sender) - NULL if cannot be determined';
COMMENT ON COLUMN transactions.wallet_address IS 'Destination wallet address (receiver/monitored wallet)';
