package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/urfave/cli/v2"
)

func listWalletsCommand() *cli.Command {
	return &cli.Command{
		Name:    "list-wallets",
		Usage:   "List all registered wallets",
		Aliases: []string{"ls"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "status",
				Aliases: []string{"s"},
				Usage:   "Filter by status (active, paused, error)",
			},
		},
		Action: func(c *cli.Context) error {
			store, closer, err := getStore(c)
			if err != nil {
				return err
			}
			defer closer()

			wallets, err := store.ListWallets(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list wallets: %w", err)
			}

			// Filter by status if specified
			statusFilter := c.String("status")
			if statusFilter != "" {
				filtered := make([]*db.Wallet, 0)
				for _, w := range wallets {
					if w.Status == statusFilter {
						filtered = append(filtered, w)
					}
				}
				wallets = filtered
			}

			if c.Bool("json") {
				return outputJSON(wallets)
			}

			// Pretty table output
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ADDRESS\tSTATUS\tPOLL INTERVAL\tLAST POLL\tCREATED")
			for _, wallet := range wallets {
				lastPoll := "never"
				if wallet.LastPollTime != nil {
					lastPoll = wallet.LastPollTime.Format(time.RFC3339)
				}
				fmt.Fprintf(w, "%s\t%s\t%v\t%s\t%s\n",
					wallet.Address,
					wallet.Status,
					wallet.PollInterval,
					lastPoll,
					wallet.CreatedAt.Format(time.RFC3339),
				)
			}
			w.Flush()

			fmt.Fprintf(os.Stderr, "\nTotal: %d wallets\n", len(wallets))
			return nil
		},
	}
}

func getWalletCommand() *cli.Command {
	return &cli.Command{
		Name:      "get-wallet",
		Usage:     "Get wallet details",
		Aliases:   []string{"get"},
		ArgsUsage: "<address>",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("requires exactly one argument: wallet address")
			}

			address := c.Args().First()
			store, closer, err := getStore(c)
			if err != nil {
				return err
			}
			defer closer()

			wallet, err := store.GetWallet(context.Background(), address)
			if err != nil {
				return fmt.Errorf("failed to get wallet: %w", err)
			}

			if c.Bool("json") {
				return outputJSON(wallet)
			}

			// Pretty output
			fmt.Printf("Address:       %s\n", wallet.Address)
			fmt.Printf("Status:        %s\n", wallet.Status)
			fmt.Printf("Poll Interval: %v\n", wallet.PollInterval)
			if wallet.LastPollTime != nil {
				fmt.Printf("Last Poll:     %s\n", wallet.LastPollTime.Format(time.RFC3339))
			} else {
				fmt.Printf("Last Poll:     never\n")
			}
			fmt.Printf("Created:       %s\n", wallet.CreatedAt.Format(time.RFC3339))
			fmt.Printf("Updated:       %s\n", wallet.UpdatedAt.Format(time.RFC3339))

			return nil
		},
	}
}

func listTransactionsCommand() *cli.Command {
	return &cli.Command{
		Name:    "list-transactions",
		Usage:   "List transactions",
		Aliases: []string{"txs"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "wallet",
				Aliases: []string{"w"},
				Usage:   "Filter by wallet address",
			},
			&cli.StringFlag{
				Name:  "since",
				Usage: "Show transactions since this time (RFC3339 format)",
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"n"},
				Usage:   "Limit number of transactions",
				Value:   50,
			},
		},
		Action: func(c *cli.Context) error {
			store, closer, err := getStore(c)
			if err != nil {
				return err
			}
			defer closer()

			var transactions []*db.Transaction

			walletAddr := c.String("wallet")
			sinceStr := c.String("since")

			if walletAddr != "" && sinceStr != "" {
				// Filter by wallet and time
				since, err := time.Parse(time.RFC3339, sinceStr)
				if err != nil {
					return fmt.Errorf("invalid time format (use RFC3339): %w", err)
				}
				transactions, err = store.GetTransactionsSince(context.Background(), walletAddr, since)
				if err != nil {
					return fmt.Errorf("failed to get transactions: %w", err)
				}
			} else if walletAddr != "" {
				// Filter by wallet only
				params := db.ListTransactionsByWalletParams{
					WalletAddress: walletAddr,
					Limit:         int32(c.Int("limit")),
					Offset:        0,
				}
				transactions, err = store.ListTransactionsByWallet(context.Background(), params)
				if err != nil {
					return fmt.Errorf("failed to get transactions: %w", err)
				}
			} else {
				return fmt.Errorf("please specify --wallet flag to list transactions")
			}

			if c.Bool("json") {
				return outputJSON(transactions)
			}

			// Pretty table output
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SIGNATURE\tWALLET\tBLOCK TIME\tSLOT\tAMOUNT")
			for _, tx := range transactions {
				sigDisplay := tx.Signature
				if len(sigDisplay) > 16 {
					sigDisplay = sigDisplay[:16] + "..."
				}
				walletDisplay := tx.WalletAddress
				if len(walletDisplay) > 16 {
					walletDisplay = walletDisplay[:16] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
					sigDisplay,
					walletDisplay,
					tx.BlockTime.Format("2006-01-02 15:04:05"),
					tx.Slot,
					tx.Amount,
				)
			}
			w.Flush()

			fmt.Fprintf(os.Stderr, "\nTotal: %d transactions\n", len(transactions))
			return nil
		},
	}
}

// Helper function to connect to database
func getStore(c *cli.Context) (*db.Store, func(), error) {
	dbURL := c.String("database-url")
	if dbURL == "" {
		return nil, nil, fmt.Errorf("database-url is required (set DATABASE_URL env var or use --database-url)")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := db.NewStore(pool)
	closer := func() { pool.Close() }

	return store, closer, nil
}

// Helper function to output JSON
func outputJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
