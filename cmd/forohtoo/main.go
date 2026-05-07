package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v2"
)

var (
	// Version information (set via ldflags during build)
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	app := &cli.App{
		Name:  "forohtoo",
		Usage: "Solana wallet payment monitoring service CLI",
		Description: `A command-line tool for managing and debugging the forohtoo service.

Use this CLI to inspect database state and stream transactions.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		Commands: []*cli.Command{
			// Database inspection commands
			{
				Name:  "db",
				Usage: "Database inspection commands",
				Subcommands: []*cli.Command{
					listWalletsCommand(),
					getWalletCommand(),
					listTransactionsCommand(),
				},
			},
			// NATS transaction streaming commands
			{
				Name:  "nats",
				Usage: "NATS transaction streaming commands",
				Subcommands: []*cli.Command{
					subscribeCommand(),
					smokeTestCommand(),
					inspectStreamCommand(),
				},
			},
			// SSE streaming commands
			sseCommands(),
			// Wallet transaction monitoring commands
			walletCommands(),
			// Helius webhook management commands
			heliusCommands(),
			// Server utility commands
			{
				Name:  "server",
				Usage: "Server utility commands",
				Subcommands: []*cli.Command{
					healthCommand(),
					versionCommand(),
				},
			},
		},
		// Global flags available to all commands
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "database-url",
				Usage:   "Database connection URL",
				EnvVars: []string{"DATABASE_URL"},
			},
			&cli.StringFlag{
				Name:    "server-url",
				Usage:   "Server URL for health checks",
				EnvVars: []string{"SERVER_URL"},
				Value:   "http://localhost:8080",
			},
			&cli.StringFlag{
				Name:    "nats-url",
				Usage:   "NATS server URL",
				EnvVars: []string{"NATS_URL"},
				Value:   "nats://localhost:4222",
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output in JSON format",
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
