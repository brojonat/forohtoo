package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/db"
	solanago "github.com/gagliardetto/solana-go"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateWalletsToAssets migrates existing wallet records to include USDC asset information.
// This populates token_mint and associated_token_address for all existing wallets.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting wallet asset migration")

	// Load configuration
	cfg := config.MustLoad()

	// Connect to database
	ctx := context.Background()
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	// Verify database connection
	if err := dbPool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Create store
	store := db.NewStore(dbPool)

	// Get USDC mint addresses from config
	usdcMainnetMint := cfg.USDCMainnetMintAddress
	usdcDevnetMint := cfg.USDCDevnetMintAddress

	if usdcMainnetMint == "" || usdcDevnetMint == "" {
		logger.Error("USDC mint addresses not configured")
		os.Exit(1)
	}

	logger.Info("using USDC mint addresses",
		"mainnet", usdcMainnetMint,
		"devnet", usdcDevnetMint,
	)

	// Fetch all existing wallets
	// Note: This query will fetch wallets before the migration, so they won't have asset columns yet
	rows, err := dbPool.Query(ctx, "SELECT address, network FROM wallets ORDER BY created_at")
	if err != nil {
		logger.Error("failed to query wallets", "error", err)
		os.Exit(1)
	}
	defer rows.Close()

	// Collect wallets
	type wallet struct {
		address string
		network string
	}
	var wallets []wallet

	for rows.Next() {
		var w wallet
		if err := rows.Scan(&w.address, &w.network); err != nil {
			logger.Error("failed to scan wallet row", "error", err)
			os.Exit(1)
		}
		wallets = append(wallets, w)
	}

	if err := rows.Err(); err != nil {
		logger.Error("error iterating wallet rows", "error", err)
		os.Exit(1)
	}

	logger.Info("found existing wallets", "count", len(wallets))

	// Update each wallet with USDC asset information
	successCount := 0
	errorCount := 0

	for _, w := range wallets {
		// Determine USDC mint based on network
		var usdcMint string
		switch w.network {
		case "mainnet":
			usdcMint = usdcMainnetMint
		case "devnet":
			usdcMint = usdcDevnetMint
		default:
			logger.Warn("unknown network, skipping wallet", "address", w.address, "network", w.network)
			errorCount++
			continue
		}

		// Compute associated token account (ATA)
		walletPubkey, err := solanago.PublicKeyFromBase58(w.address)
		if err != nil {
			logger.Error("failed to parse wallet address", "address", w.address, "error", err)
			errorCount++
			continue
		}

		mintPubkey, err := solanago.PublicKeyFromBase58(usdcMint)
		if err != nil {
			logger.Error("failed to parse mint address", "mint", usdcMint, "error", err)
			errorCount++
			continue
		}

		ata, _, err := solanago.FindAssociatedTokenAddress(walletPubkey, mintPubkey)
		if err != nil {
			logger.Error("failed to derive ATA", "address", w.address, "mint", usdcMint, "error", err)
			errorCount++
			continue
		}

		// Update the wallet row with asset information
		query := `
			UPDATE wallets
			SET
				token_mint = $1,
				associated_token_address = $2
			WHERE address = $3 AND network = $4
		`
		_, err = dbPool.Exec(ctx, query, usdcMint, ata.String(), w.address, w.network)
		if err != nil {
			logger.Error("failed to update wallet",
				"address", w.address,
				"network", w.network,
				"error", err,
			)
			errorCount++
			continue
		}

		logger.Info("migrated wallet",
			"address", w.address,
			"network", w.network,
			"mint", usdcMint,
			"ata", ata.String(),
		)
		successCount++
	}

	logger.Info("migration complete",
		"total", len(wallets),
		"success", successCount,
		"errors", errorCount,
	)

	if errorCount > 0 {
		os.Exit(1)
	}
}
