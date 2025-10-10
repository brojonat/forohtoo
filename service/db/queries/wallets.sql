-- name: CreateWallet :one
INSERT INTO wallets (
    address,
    poll_interval,
    status
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetWallet :one
SELECT * FROM wallets
WHERE address = $1;

-- name: ListWallets :many
SELECT * FROM wallets
ORDER BY created_at DESC;

-- name: ListActiveWallets :many
SELECT * FROM wallets
WHERE status = 'active'
ORDER BY last_poll_time ASC NULLS FIRST;

-- name: UpdateWalletPollTime :one
UPDATE wallets
SET
    last_poll_time = $2,
    updated_at = NOW()
WHERE address = $1
RETURNING *;

-- name: UpdateWalletStatus :one
UPDATE wallets
SET
    status = $2,
    updated_at = NOW()
WHERE address = $1
RETURNING *;

-- name: DeleteWallet :exec
DELETE FROM wallets
WHERE address = $1;

-- name: WalletExists :one
SELECT EXISTS(SELECT 1 FROM wallets WHERE address = $1);
