package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/urfave/cli/v2"
)

func clientCommands() *cli.Command {
	return &cli.Command{
		Name:  "client",
		Usage: "HTTP client commands for interacting with the forohtoo service",
		Subcommands: []*cli.Command{
			awaitCommand(),
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
				Value:   "http://localhost:8080",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.StringFlag{
				Name:    "workflow-id",
				Aliases: []string{"w"},
				Usage:   "Filter by workflow_id in transaction memo",
			},
			&cli.StringFlag{
				Name:    "signature",
				Usage:   "Filter by exact transaction signature",
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
			workflowID := c.String("workflow-id")
			signature := c.String("signature")
			timeout := c.Duration("timeout")
			jsonOutput := c.Bool("json")

			if workflowID == "" && signature == "" {
				return fmt.Errorf("must specify --workflow-id or --signature")
			}

			// Create HTTP client with appropriate timeout
			httpClient := &http.Client{
				Timeout: timeout + 30*time.Second, // Add buffer beyond server timeout
			}

			// Create logger
			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError, // Only errors to stderr
			}))

			// Create client
			cl := client.NewClient(serverURL, httpClient, logger)

			// Build await options
			opts := client.AwaitOptions{
				Timeout: timeout,
			}
			if workflowID != "" {
				opts.WorkflowID = &workflowID
			}
			if signature != "" {
				opts.Signature = &signature
			}

			// Print waiting message
			if !jsonOutput {
				fmt.Fprintf(os.Stderr, "Waiting for transaction on wallet %s...\n", address)
				if workflowID != "" {
					fmt.Fprintf(os.Stderr, "  Workflow ID: %s\n", workflowID)
				}
				if signature != "" {
					fmt.Fprintf(os.Stderr, "  Signature: %s\n", signature)
				}
				fmt.Fprintf(os.Stderr, "  Timeout: %v\n\n", timeout)
			}

			// Block until transaction arrives
			ctx := context.Background()
			txn, err := cl.Await(ctx, address, opts)
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

func printTransactionDetailed(txn *client.Transaction) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✓ Transaction Received")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Signature:   %s\n", txn.Signature)
	fmt.Printf("Wallet:      %s\n", txn.WalletAddress)
	fmt.Printf("Amount:      %.4f SOL\n", float64(txn.Amount)/1e9)
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
