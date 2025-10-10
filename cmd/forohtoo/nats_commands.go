package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	natspkg "github.com/brojonat/forohtoo/service/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/urfave/cli/v2"
)

// subscribeCommand subscribes to transaction events for a wallet.
func subscribeCommand() *cli.Command {
	return &cli.Command{
		Name:      "subscribe",
		Usage:     "Subscribe to transaction events for a wallet",
		ArgsUsage: "[wallet_address]",
		Description: `Subscribe to real-time transaction events published to NATS JetStream.

This command connects to NATS and streams transaction events for the specified wallet address.
Events are published to the subject: txns.{wallet_address}

Example:
  forohtoo nats subscribe DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK --json`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "nats-url",
				Usage:   "NATS server URL",
				EnvVars: []string{"NATS_URL"},
				Value:   "nats://localhost:4222",
			},
			&cli.BoolFlag{
				Name:    "durable",
				Aliases: []string{"d"},
				Usage:   "Create a durable consumer (survives restarts)",
			},
			&cli.StringFlag{
				Name:    "consumer-name",
				Usage:   "Consumer name (required for durable)",
				Value:   "forohtoo-cli",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("wallet address is required")
			}

			address := c.Args().Get(0)
			natsURL := c.String("nats-url")
			durable := c.Bool("durable")
			consumerName := c.String("consumer-name")
			jsonOutput := c.Bool("json")

			return streamTransactions(address, natsURL, durable, consumerName, jsonOutput)
		},
	}
}

// smokeTestCommand runs a smoke test by subscribing to a known busy wallet.
func smokeTestCommand() *cli.Command {
	return &cli.Command{
		Name:  "smoke-test",
		Usage: "Run a smoke test by streaming transactions from a busy wallet",
		Description: `Smoke test the entire system by:
1. Subscribing to NATS transaction stream
2. Waiting for transaction events

This uses a known busy Solana wallet to verify the system is working end-to-end.

Example:
  forohtoo nats smoke-test --timeout 60s`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "nats-url",
				Usage:   "NATS server URL",
				EnvVars: []string{"NATS_URL"},
				Value:   "nats://localhost:4222",
			},
			&cli.StringFlag{
				Name:  "wallet",
				Usage: "Wallet address to monitor (uses a busy wallet by default)",
				// Pump.fun bonding curve wallet - very active
				Value: "CebN5WGQ4jvEPvsVU4EoHEpgzq1VV7AbicfhtW4xC9iM",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "How long to wait for transactions",
				Value: 30 * time.Second,
			},
		},
		Action: func(c *cli.Context) error {
			address := c.String("wallet")
			natsURL := c.String("nats-url")
			timeout := c.Duration("timeout")
			jsonOutput := c.Bool("json")

			if !jsonOutput {
				fmt.Printf("🧪 Smoke test starting...\n")
				fmt.Printf("   Wallet: %s\n", address)
				fmt.Printf("   NATS: %s\n", natsURL)
				fmt.Printf("   Timeout: %s\n\n", timeout)
			}

			// Connect to NATS
			nc, err := nats.Connect(natsURL)
			if err != nil {
				return fmt.Errorf("failed to connect to NATS: %w", err)
			}
			defer nc.Close()

			js, err := jetstream.New(nc)
			if err != nil {
				return fmt.Errorf("failed to create JetStream context: %w", err)
			}

			subject := fmt.Sprintf("txns.%s", address)

			if !jsonOutput {
				fmt.Printf("📡 Subscribing to: %s\n\n", subject)
			}

			// Create consumer
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			cons, err := js.CreateOrUpdateConsumer(ctx, natspkg.StreamName, jetstream.ConsumerConfig{
				FilterSubject: subject,
				AckPolicy:     jetstream.AckExplicitPolicy,
			})
			if err != nil {
				return fmt.Errorf("failed to create consumer: %w", err)
			}

			// Receive messages
			received := 0
			msgChan := make(chan jetstream.Msg, 10)

			// Start consuming in background
			go func() {
				_, _ = cons.Consume(func(msg jetstream.Msg) {
					msgChan <- msg
				})
			}()

			for {
				select {
				case msg := <-msgChan:
					var event natspkg.TransactionEvent
					if err := json.Unmarshal(msg.Data(), &event); err != nil {
						fmt.Fprintf(os.Stderr, "Error parsing event: %v\n", err)
						msg.Ack()
						continue
					}

					received++

					if jsonOutput {
						data, _ := json.Marshal(event)
						fmt.Println(string(data))
					} else {
						fmt.Printf("✅ Transaction received (#%d)\n", received)
						fmt.Printf("   Signature: %s\n", event.Signature)
						fmt.Printf("   Amount: %d lamports\n", event.Amount)
						fmt.Printf("   Slot: %d\n", event.Slot)
						if event.Memo != "" {
							fmt.Printf("   Memo: %s\n", event.Memo)
						}
						fmt.Printf("   Published: %s\n\n", event.PublishedAt.Format(time.RFC3339))
					}

					msg.Ack()

				case <-ctx.Done():
					if !jsonOutput {
						if received == 0 {
							fmt.Printf("⚠️  Timeout: No transactions received in %s\n", timeout)
							fmt.Printf("\nPossible issues:\n")
							fmt.Printf("  - Worker may not be running\n")
							fmt.Printf("  - Wallet may not be registered (check: forohtoo db list-wallets)\n")
							fmt.Printf("  - NATS stream may not exist (check NATS logs)\n")
							return fmt.Errorf("smoke test failed: no transactions received")
						}
						fmt.Printf("✅ Smoke test passed: Received %d transaction(s)\n", received)
					}
					return nil
				}
			}
		},
	}
}

// streamTransactions connects to NATS and streams transaction events.
func streamTransactions(address, natsURL string, durable bool, consumerName string, jsonOutput bool) error {
	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	subject := fmt.Sprintf("txns.%s", address)

	if !jsonOutput {
		fmt.Printf("📡 Subscribing to: %s\n", subject)
		fmt.Printf("   NATS: %s\n", natsURL)
		if durable {
			fmt.Printf("   Consumer: %s (durable)\n", consumerName)
		}
		fmt.Printf("\nWaiting for transactions... (Ctrl-C to exit)\n\n")
	}

	// Create consumer config
	consumerConfig := jetstream.ConsumerConfig{
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	}

	if durable {
		consumerConfig.Durable = consumerName
		consumerConfig.Name = consumerName
	}

	// Create or update consumer
	cons, err := js.CreateOrUpdateConsumer(context.Background(), natspkg.StreamName, consumerConfig)
	if err != nil {
		return fmt.Errorf("failed to create consumer: %w", err)
	}

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Receive messages
	msgChan := make(chan jetstream.Msg, 10)

	// Start consuming in background
	go func() {
		_, _ = cons.Consume(func(msg jetstream.Msg) {
			msgChan <- msg
		})
	}()

	count := 0
	for {
		select {
		case msg := <-msgChan:
			var event natspkg.TransactionEvent
			if err := json.Unmarshal(msg.Data(), &event); err != nil {
				if !jsonOutput {
					fmt.Fprintf(os.Stderr, "Error parsing event: %v\n", err)
				}
				msg.Ack()
				continue
			}

			count++

			if jsonOutput {
				// Output raw JSON
				data, _ := json.Marshal(event)
				fmt.Println(string(data))
			} else {
				// Human-friendly output
				fmt.Printf("─────────────────────────────────────────────────────\n")
				fmt.Printf("Transaction #%d\n", count)
				fmt.Printf("─────────────────────────────────────────────────────\n")
				fmt.Printf("Signature:    %s\n", event.Signature)
				fmt.Printf("Wallet:       %s\n", event.WalletAddress)
				fmt.Printf("Amount:       %d lamports\n", event.Amount)
				fmt.Printf("Slot:         %d\n", event.Slot)
				fmt.Printf("Status:       %s\n", event.ConfirmationStatus)
				fmt.Printf("Block Time:   %s\n", event.BlockTime.Format(time.RFC3339))
				if event.TokenType != "" {
					fmt.Printf("Token:        %s\n", event.TokenType)
				}
				if event.Memo != "" {
					fmt.Printf("Memo:         %s\n", event.Memo)
				}
				fmt.Printf("Published:    %s\n", event.PublishedAt.Format(time.RFC3339))
				fmt.Printf("\n")
			}

			msg.Ack()

		case <-sigChan:
			if !jsonOutput {
				fmt.Printf("\n\n✅ Received %d transactions\n", count)
				fmt.Println("Shutting down...")
			}
			return nil
		}
	}
}

// inspectStreamCommand shows information about the NATS JetStream stream.
func inspectStreamCommand() *cli.Command {
	return &cli.Command{
		Name:  "inspect-stream",
		Usage: "Inspect the TRANSACTIONS JetStream stream",
		Description: `Show information about the JetStream stream including:
- Message count
- Consumers
- Storage usage
- Stream configuration

Example:
  forohtoo nats inspect-stream`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "nats-url",
				Usage:   "NATS server URL",
				EnvVars: []string{"NATS_URL"},
				Value:   "nats://localhost:4222",
			},
		},
		Action: func(c *cli.Context) error {
			natsURL := c.String("nats-url")
			jsonOutput := c.Bool("json")

			// Connect to NATS
			nc, err := nats.Connect(natsURL)
			if err != nil {
				return fmt.Errorf("failed to connect to NATS: %w", err)
			}
			defer nc.Close()

			js, err := jetstream.New(nc)
			if err != nil {
				return fmt.Errorf("failed to create JetStream context: %w", err)
			}

			// Get stream info
			stream, err := js.Stream(context.Background(), natspkg.StreamName)
			if err != nil {
				return fmt.Errorf("failed to get stream: %w", err)
			}

			info, err := stream.Info(context.Background())
			if err != nil {
				return fmt.Errorf("failed to get stream info: %w", err)
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(info, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Printf("Stream: %s\n", info.Config.Name)
				fmt.Printf("─────────────────────────────────────────────────────\n")
				fmt.Printf("Description:  %s\n", info.Config.Description)
				fmt.Printf("Subjects:     %v\n", info.Config.Subjects)
				fmt.Printf("Messages:     %d\n", info.State.Msgs)
				fmt.Printf("Bytes:        %d\n", info.State.Bytes)
				fmt.Printf("First Seq:    %d\n", info.State.FirstSeq)
				fmt.Printf("Last Seq:     %d\n", info.State.LastSeq)
				fmt.Printf("Consumers:    %d\n", info.State.Consumers)
				fmt.Printf("Max Age:      %s\n", info.Config.MaxAge)
				fmt.Printf("Storage:      %s\n", info.Config.Storage)
				fmt.Printf("\n")
			}

			return nil
		},
	}
}
