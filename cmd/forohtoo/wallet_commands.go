package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/itchyny/gojq"
	"github.com/urfave/cli/v2"
)

func walletCommands() *cli.Command {
	return &cli.Command{
		Name:  "wallet",
		Usage: "Wallet transaction monitoring commands",
		Subcommands: []*cli.Command{
			walletAddCommand(),
			walletRemoveCommand(),
			walletGetCommand(),
			walletListCommand(),
			walletTransactionsCommand(),
			awaitCommand(),
		},
	}
}

func walletAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Aliases:   []string{"register"},
		Usage:     "Register a wallet for monitoring",
		ArgsUsage: "WALLET_ADDRESS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.DurationFlag{
				Name:    "poll-interval",
				Aliases: []string{"i"},
				Value:   30 * time.Second,
				Usage:   "How often to poll for new transactions (e.g., 30s, 1m)",
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			serverURL := c.String("server")
			pollInterval := c.Duration("poll-interval")
			jsonOutput := c.Bool("json")

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			cl := client.NewClient(serverURL, nil, logger)

			if err := cl.Register(context.Background(), address, pollInterval); err != nil {
				return fmt.Errorf("failed to register wallet: %w", err)
			}

			if jsonOutput {
				data, _ := json.Marshal(map[string]interface{}{
					"address":       address,
					"poll_interval": pollInterval.String(),
					"status":        "registered",
				})
				fmt.Println(string(data))
			} else {
				fmt.Printf("✓ Wallet registered successfully\n")
				fmt.Printf("  Address: %s\n", address)
				fmt.Printf("  Poll Interval: %s\n", pollInterval)
			}

			return nil
		},
	}
}

func walletRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Aliases:   []string{"rm", "delete", "unregister"},
		Usage:     "Unregister a wallet from monitoring",
		ArgsUsage: "WALLET_ADDRESS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			serverURL := c.String("server")
			jsonOutput := c.Bool("json")

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			cl := client.NewClient(serverURL, nil, logger)

			if err := cl.Unregister(context.Background(), address); err != nil {
				return fmt.Errorf("failed to unregister wallet: %w", err)
			}

			if jsonOutput {
				data, _ := json.Marshal(map[string]interface{}{
					"address": address,
					"status":  "unregistered",
				})
				fmt.Println(string(data))
			} else {
				fmt.Printf("✓ Wallet unregistered successfully\n")
				fmt.Printf("  Address: %s\n", address)
			}

			return nil
		},
	}
}

func walletGetCommand() *cli.Command {
	return &cli.Command{
		Name:      "get",
		Aliases:   []string{"show"},
		Usage:     "Get details for a specific wallet",
		ArgsUsage: "WALLET_ADDRESS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			serverURL := c.String("server")
			jsonOutput := c.Bool("json")

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			cl := client.NewClient(serverURL, nil, logger)

			wallet, err := cl.Get(context.Background(), address)
			if err != nil {
				return fmt.Errorf("failed to get wallet: %w", err)
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(wallet, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
				fmt.Println("Wallet Details")
				fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
				fmt.Printf("Address:       %s\n", wallet.Address)
				fmt.Printf("Status:        %s\n", wallet.Status)
				fmt.Printf("Poll Interval: %s\n", wallet.PollInterval)
				if wallet.LastPollTime != nil {
					fmt.Printf("Last Poll:     %s\n", wallet.LastPollTime.Format(time.RFC3339))
				} else {
					fmt.Printf("Last Poll:     (never)\n")
				}
				fmt.Printf("Created At:    %s\n", wallet.CreatedAt.Format(time.RFC3339))
				fmt.Printf("Updated At:    %s\n", wallet.UpdatedAt.Format(time.RFC3339))
				fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			}

			return nil
		},
	}
}

func walletListCommand() *cli.Command {
	return &cli.Command{
		Name:    "list",
		Aliases: []string{"ls"},
		Usage:   "List all registered wallets (outputs JSON by default)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.BoolFlag{
				Name:    "table",
				Aliases: []string{"t"},
				Usage:   "Output as human-readable table instead of JSON",
			},
		},
		Action: func(c *cli.Context) error {
			serverURL := c.String("server")
			tableOutput := c.Bool("table")

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			cl := client.NewClient(serverURL, nil, logger)

			wallets, err := cl.List(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list wallets: %w", err)
			}

			// Default to JSON output
			if !tableOutput {
				data, _ := json.MarshalIndent(wallets, "", "  ")
				fmt.Println(string(data))
			} else {
				// Table output
				if len(wallets) == 0 {
					fmt.Println("No wallets registered")
					return nil
				}

				fmt.Printf("Found %d wallet(s):\n\n", len(wallets))
				for _, w := range wallets {
					fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
					fmt.Printf("Address:       %s\n", w.Address)
					fmt.Printf("Status:        %s\n", w.Status)
					fmt.Printf("Poll Interval: %s\n", w.PollInterval)
					if w.LastPollTime != nil {
						fmt.Printf("Last Poll:     %s\n", w.LastPollTime.Format(time.RFC3339))
					} else {
						fmt.Printf("Last Poll:     (never)\n")
					}
					fmt.Println()
				}
				fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
			}

			return nil
		},
	}
}

func awaitCommand() *cli.Command {
	return &cli.Command{
		Name:      "await",
		Usage:     "Block until a transaction matching criteria arrives",
		ArgsUsage: "WALLET_ADDRESS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.StringFlag{
				Name:  "signature",
				Usage: "Filter by exact transaction signature",
			},
			&cli.Float64Flag{
				Name:  "usdc-amount-equal",
				Usage: "Filter by exact USDC amount (e.g., 0.42 for 0.42 USDC). Requires USDC_MINT_ADDRESS env var.",
			},
			&cli.StringSliceFlag{
				Name:    "must-jq",
				Usage:   "jq filter expression that must evaluate to true (can be specified multiple times, all must match)",
				Aliases: []string{"jq"},
			},
			&cli.DurationFlag{
				Name:    "timeout",
				Aliases: []string{"t"},
				Value:   5 * time.Minute,
				Usage:   "How long to wait for transaction (default: 5m, max: 10m)",
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output transaction as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			serverURL := c.String("server")
			signature := c.String("signature")
			usdcAmount := c.Float64("usdc-amount-equal")
			jqFilters := c.StringSlice("must-jq")
			timeout := c.Duration("timeout")
			jsonOutput := c.Bool("json")

			// Require at least one filter
			if signature == "" && usdcAmount == 0 && len(jqFilters) == 0 {
				return fmt.Errorf("must specify at least one filter: --signature, --usdc-amount-equal, or --must-jq")
			}

			// If using USDC amount filter, require USDC mint address from env
			var usdcMintAddress string
			if usdcAmount != 0 {
				usdcMintAddress = os.Getenv("USDC_MINT_ADDRESS")
				if usdcMintAddress == "" {
					return fmt.Errorf("--usdc-amount-equal requires USDC_MINT_ADDRESS environment variable to be set")
				}
			}

			// Create logger
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError, // Only errors to stderr
			}))

			// Compile jq filters
			compiledJQFilters := make([]*gojq.Code, len(jqFilters))
			for i, filter := range jqFilters {
				query, err := gojq.Parse(filter)
				if err != nil {
					return fmt.Errorf("failed to parse jq filter %q: %w", filter, err)
				}
				compiledJQFilters[i], err = gojq.Compile(query)
				if err != nil {
					return fmt.Errorf("failed to compile jq filter %q: %w", filter, err)
				}
			}

			// Create client
			cl := client.NewClient(serverURL, nil, logger)

			// Build matcher function based on flags
			matcher := func(txn *client.Transaction) bool {
				// Check signature match
				if signature != "" && txn.Signature != signature {
					return false
				}

				// Check USDC amount (USDC has 6 decimals)
				if usdcAmount != 0 {
					// Verify it's actually USDC by checking token_type (which contains the mint address)
					if txn.TokenType != usdcMintAddress {
						return false
					}

					// Check amount matches (USDC has 6 decimals)
					expectedLamports := int64(usdcAmount * 1e6)
					if txn.Amount != expectedLamports {
						return false
					}
				}

				// Check jq filters (all must return true)
				if len(compiledJQFilters) > 0 {
					// Parse memo as JSON for jq filtering
					var memoJSON interface{}
					if err := json.Unmarshal([]byte(txn.Memo), &memoJSON); err != nil {
						// If memo is not valid JSON, jq filters will fail
						return false
					}

					// All jq filters must evaluate to true
					for _, code := range compiledJQFilters {
						iter := code.Run(memoJSON)
						v, ok := iter.Next()
						if !ok {
							// No result means filter failed
							return false
						}
						if err, isErr := v.(error); isErr {
							// Filter error means it failed
							logger.Debug("jq filter error", "error", err)
							return false
						}
						// Check if result is truthy (true, non-zero number, non-empty string, etc.)
						if !isTruthy(v) {
							return false
						}
					}
				}

				return true
			}

			// Print waiting message
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Waiting for transaction on wallet %s...\n", address)
				if signature != "" {
					fmt.Fprintf(os.Stderr, "  Signature: %s\n", signature)
				}
				if usdcAmount != 0 {
					fmt.Fprintf(os.Stderr, "  USDC Amount: %.6f USDC\n", usdcAmount)
				}
				for _, filter := range jqFilters {
					fmt.Fprintf(os.Stderr, "  jq Filter: %s\n", filter)
				}
				fmt.Fprintf(os.Stderr, "  Timeout: %v\n\n", timeout)
			}

			// Block until transaction arrives (with context timeout)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			txn, err := cl.Await(ctx, address, matcher)
			if err != nil {
				return fmt.Errorf("failed to await transaction: %w", err)
			}

			// Output transaction
			if jsonOutput {
				data, err := json.MarshalIndent(txn, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal transaction: %w", err)
				}
				fmt.Println(string(data))
			} else {
				printTransactionDetailed(txn)
			}

			return nil
		},
	}
}

// isTruthy checks if a jq result value is truthy.
// In jq, false and null are falsy, everything else is truthy.
func isTruthy(v interface{}) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	// Everything else (numbers, strings, objects, arrays) is truthy
	return true
}

func walletTransactionsCommand() *cli.Command {
	return &cli.Command{
		Name:      "transactions",
		Aliases:   []string{"txns", "tx"},
		Usage:     "List transactions for a wallet",
		ArgsUsage: "WALLET_ADDRESS",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "https://forohtoo.brojonat.com",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"l"},
				Value:   20,
				Usage:   "Maximum number of transactions to retrieve (1-1000)",
			},
			&cli.IntFlag{
				Name:    "offset",
				Aliases: []string{"o"},
				Value:   0,
				Usage:   "Number of transactions to skip",
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output as JSON",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			serverURL := c.String("server")
			limit := c.Int("limit")
			offset := c.Int("offset")
			jsonOutput := c.Bool("json")

			if limit < 1 || limit > 1000 {
				return fmt.Errorf("limit must be between 1 and 1000")
			}
			if offset < 0 {
				return fmt.Errorf("offset cannot be negative")
			}

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			cl := client.NewClient(serverURL, nil, logger)

			transactions, err := cl.ListTransactions(context.Background(), address, limit, offset)
			if err != nil {
				return fmt.Errorf("failed to list transactions: %w", err)
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(transactions, "", "  ")
				fmt.Println(string(data))
			} else {
				if len(transactions) == 0 {
					fmt.Println("No transactions found")
					return nil
				}

				fmt.Printf("Found %d transaction(s) for wallet %s:\n\n", len(transactions), address)
				for i, txn := range transactions {
					fmt.Printf("[%d] Signature: %s\n", i+1, txn.Signature)
					if txn.FromAddress != nil {
						fmt.Printf("    From:      %s\n", *txn.FromAddress)
					}
					fmt.Printf("    To:        %s\n", txn.WalletAddress)

					// Format amount based on token type
					amount, token := formatAmount(txn.Amount, txn.TokenType)
					fmt.Printf("    Amount:    %s %s\n", amount, token)

					fmt.Printf("    Slot:      %d\n", txn.Slot)
					fmt.Printf("    Status:    %s\n", txn.ConfirmationStatus)
					if !txn.BlockTime.IsZero() {
						fmt.Printf("    Block Time: %s\n", txn.BlockTime.Format(time.RFC3339))
					}
					if txn.TokenType != "" {
						fmt.Printf("    Token:     %s\n", txn.TokenType)
					}
					if txn.Memo != "" {
						fmt.Printf("    Memo:      %s\n", txn.Memo)
					}
					if !txn.PublishedAt.IsZero() {
						fmt.Printf("    Published: %s\n", txn.PublishedAt.Format(time.RFC3339))
					}
					fmt.Println()
				}
			}

			return nil
		},
	}
}

func printTransactionDetailed(txn *client.Transaction) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✓ Transaction Received")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Signature:   %s\n", txn.Signature)
	if txn.FromAddress != nil {
		fmt.Printf("From:        %s\n", *txn.FromAddress)
	}
	fmt.Printf("To:          %s\n", txn.WalletAddress)

	// Format amount based on token type
	amount, token := formatAmount(txn.Amount, txn.TokenType)
	fmt.Printf("Amount:      %s %s\n", amount, token)

	fmt.Printf("Slot:        %d\n", txn.Slot)
	fmt.Printf("Status:      %s\n", txn.ConfirmationStatus)

	if !txn.BlockTime.IsZero() {
		fmt.Printf("Block Time:  %s\n", txn.BlockTime.Format(time.RFC3339))
	}

	if txn.TokenType != "" {
		fmt.Printf("Token:       %s\n", txn.TokenType)
	}

	if txn.Memo != "" {
		fmt.Printf("Memo:        %s\n", txn.Memo)
	}

	fmt.Printf("Published:   %s\n", txn.PublishedAt.Format(time.RFC3339))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

// formatAmount formats a transaction amount based on the token type.
// Returns the formatted amount string and token symbol.
func formatAmount(amount int64, tokenType string) (string, string) {
	// USDC mint address (6 decimals)
	usdcMint := os.Getenv("USDC_MINT_ADDRESS")

	if tokenType == "" {
		// Native SOL (9 decimals)
		return fmt.Sprintf("%.4f", float64(amount)/1e9), "SOL"
	}

	if tokenType == usdcMint {
		// USDC (6 decimals)
		return fmt.Sprintf("%.2f", float64(amount)/1e6), "USDC"
	}

	// Unknown SPL token - use 6 decimals as default for most SPL tokens
	return fmt.Sprintf("%.6f", float64(amount)/1e6), "SPL"
}
