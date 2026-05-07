-- name: CreateWallet :one
INSERT INTO wallets (
    address,
    network,
    asset_type,
    token_mint,
    associated_token_address,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: UpsertWallet :one
INSERT INTO wallets (
    address,
    network,
    asset_type,
    token_mint,
    associated_token_address,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (address, network, asset_type, token_mint)
DO UPDATE SET
    associated_token_address = EXCLUDED.associated_token_address,
    status = EXCLUDED.status,
    updated_at = NOW()
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
ORDER BY created_at DESC;

-- name: UpdateWalletStatus :one
UPDATE wallets
SET
    status = $5,
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
