-- name: CreateTransaction :one
INSERT INTO transactions (
    signature,
    wallet_address,
    slot,
    block_time,
    amount,
    token_mint,
    memo,
    confirmation_status,
    from_address
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetTransaction :one
SELECT * FROM transactions
WHERE signature = $1
LIMIT 1;

-- name: ListTransactionsByWallet :many
SELECT * FROM transactions
WHERE wallet_address = $1
  AND from_address IS NOT NULL
ORDER BY block_time DESC
LIMIT $2 OFFSET $3;

-- name: ListTransactionsByWalletAndTimeRange :many
SELECT * FROM transactions
WHERE wallet_address = $1
  AND block_time >= $2
  AND block_time <= $3
ORDER BY block_time DESC;

-- name: CountTransactionsByWallet :one
SELECT COUNT(*) FROM transactions
WHERE wallet_address = $1;

-- name: GetLatestTransactionByWallet :one
SELECT * FROM transactions
WHERE wallet_address = $1
ORDER BY block_time DESC
LIMIT 1;

-- name: GetTransactionsSince :many
SELECT * FROM transactions
WHERE wallet_address = $1
  AND block_time > $2
ORDER BY block_time ASC;

-- name: DeleteTransactionsOlderThan :exec
DELETE FROM transactions
WHERE block_time < $1;

-- name: GetTransactionSignaturesByWallet :many
SELECT signature FROM transactions
WHERE wallet_address = @wallet_address
AND (@since::timestamptz IS NULL OR block_time > @since)
ORDER BY block_time DESC
LIMIT @limit_count;

-- name: ListTransactionsByTimeRange :many
SELECT * FROM transactions
WHERE block_time >= @start_time::timestamptz
  AND block_time <= @end_time::timestamptz
ORDER BY block_time ASC;
