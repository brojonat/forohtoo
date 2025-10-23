-- name: CreateWallet :one
INSERT INTO wallets (
    address,
    network,
    asset_type,
    token_mint,
    associated_token_address,
    poll_interval,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetWallet :one
SELECT * FROM wallets
WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4;

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
    last_poll_time = $5,
    updated_at = NOW()
WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4
RETURNING *;

-- name: UpdateWalletStatus :one
UPDATE wallets
SET
    status = $5,
    updated_at = NOW()
WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4
RETURNING *;

-- name: UpdateWalletPollInterval :one
UPDATE wallets
SET
    poll_interval = $5,
    updated_at = NOW()
WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4
RETURNING *;

-- name: DeleteWallet :exec
DELETE FROM wallets
WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4;

-- name: WalletExists :one
SELECT EXISTS(SELECT 1 FROM wallets WHERE address = $1 AND network = $2 AND asset_type = $3 AND token_mint = $4);

-- name: ListWalletsByAddress :many
SELECT * FROM wallets
WHERE address = $1
ORDER BY network, asset_type, token_mint;

-- name: ListWalletAssets :many
SELECT * FROM wallets
WHERE address = $1 AND network = $2
ORDER BY asset_type, token_mint;
