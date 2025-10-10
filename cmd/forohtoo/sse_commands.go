package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/urfave/cli/v2"
)

func sseCommands() *cli.Command {
	return &cli.Command{
		Name:  "sse",
		Usage: "Server-Sent Events (SSE) streaming commands",
		Subcommands: []*cli.Command{
			streamCommand(),
		},
	}
}

func streamCommand() *cli.Command {
	return &cli.Command{
		Name:      "stream",
		Usage:     "Stream transactions via SSE (HTTP)",
		ArgsUsage: "[wallet_address]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server",
				Aliases: []string{"s"},
				Value:   "http://localhost:8080",
				Usage:   "HTTP server URL",
				EnvVars: []string{"FOROHTOO_SERVER_URL"},
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output transactions as JSON (one per line)",
			},
		},
		Action: func(c *cli.Context) error {
			serverURL := c.String("server")
			walletAddress := c.Args().First()
			jsonOutput := c.Bool("json")

			// Build SSE endpoint URL
			var url string
			if walletAddress != "" {
				url = fmt.Sprintf("%s/api/v1/stream/transactions/%s", serverURL, walletAddress)
			} else {
				url = fmt.Sprintf("%s/api/v1/stream/transactions", serverURL)
			}

			// Create context that cancels on interrupt
			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigChan
				cancel()
			}()

			// Create HTTP request
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			req.Header.Set("Accept", "text/event-stream")

			// Make request
			client := &http.Client{
				Timeout: 0, // No timeout for streaming
			}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to connect to SSE endpoint: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("server returned status %d", resp.StatusCode)
			}

			// Print connection info
			if !jsonOutput {
				if walletAddress != "" {
					fmt.Fprintf(os.Stderr, "Connected to SSE stream for wallet: %s\n", walletAddress)
				} else {
					fmt.Fprintf(os.Stderr, "Connected to SSE stream for all wallets\n")
				}
				fmt.Fprintf(os.Stderr, "Streaming transactions... (Ctrl+C to stop)\n\n")
			}

			// Read SSE events
			scanner := bufio.NewScanner(resp.Body)
			var currentEvent, currentData string

			for scanner.Scan() {
				line := scanner.Text()

				// Empty line indicates end of event
				if line == "" {
					if currentEvent != "" && currentData != "" {
						if err := handleSSEEvent(currentEvent, currentData, jsonOutput); err != nil {
							fmt.Fprintf(os.Stderr, "Error handling event: %v\n", err)
						}
					}
					currentEvent = ""
					currentData = ""
					continue
				}

				// Parse event line
				if strings.HasPrefix(line, "event:") {
					currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				} else if strings.HasPrefix(line, "data:") {
					currentData = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				}
			}

			if err := scanner.Err(); err != nil {
				if ctx.Err() != nil {
					// Context cancelled (user interrupt)
					if !jsonOutput {
						fmt.Fprintf(os.Stderr, "\nDisconnected\n")
					}
					return nil
				}
				return fmt.Errorf("error reading SSE stream: %w", err)
			}

			return nil
		},
	}
}

func handleSSEEvent(eventType, data string, jsonOutput bool) error {
	switch eventType {
	case "connected":
		if !jsonOutput {
			var info map[string]interface{}
			if err := json.Unmarshal([]byte(data), &info); err != nil {
				return err
			}
			if wallet, ok := info["wallet"].(string); ok {
				fmt.Fprintf(os.Stderr, "✓ Subscribed to wallet: %s\n\n", wallet)
			} else if message, ok := info["message"].(string); ok {
				fmt.Fprintf(os.Stderr, "✓ %s\n\n", message)
			}
		}
		return nil

	case "transaction":
		var txn natspkg.TransactionEvent
		if err := json.Unmarshal([]byte(data), &txn); err != nil {
			return err
		}

		if jsonOutput {
			// Output raw JSON
			fmt.Println(data)
		} else {
			// Human-friendly format
			printTransaction(txn)
		}
		return nil

	case "error":
		var errInfo map[string]interface{}
		if err := json.Unmarshal([]byte(data), &errInfo); err != nil {
			return err
		}
		return fmt.Errorf("server error: %v", errInfo["error"])

	default:
		// Unknown event type, ignore
		return nil
	}
}

func printTransaction(txn natspkg.TransactionEvent) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Signature:  %s\n", txn.Signature)
	fmt.Printf("Wallet:     %s\n", txn.WalletAddress)
	fmt.Printf("Amount:     %.4f SOL\n", float64(txn.Amount)/1e9)
	fmt.Printf("Slot:       %d\n", txn.Slot)
	fmt.Printf("Status:     %s\n", txn.ConfirmationStatus)

	if !txn.BlockTime.IsZero() {
		fmt.Printf("Block Time: %s\n", txn.BlockTime.Format(time.RFC3339))
	}

	if txn.TokenType != "" {
		fmt.Printf("Token:      %s\n", txn.TokenType)
	}

	if txn.Memo != "" {
		fmt.Printf("Memo:       %s\n", txn.Memo)
	}

	fmt.Printf("Published:  %s\n", txn.PublishedAt.Format(time.RFC3339))
	fmt.Println()
}
