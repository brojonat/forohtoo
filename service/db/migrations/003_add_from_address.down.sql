-- Remove from_address column
DROP INDEX IF EXISTS idx_transactions_from_address;
ALTER TABLE transactions DROP COLUMN IF EXISTS from_address;
