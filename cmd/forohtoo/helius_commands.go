package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"github.com/brojonat/forohtoo/service/helius"
	"github.com/urfave/cli/v2"
)

func heliusCommands() *cli.Command {
	return &cli.Command{
		Name:  "helius",
		Usage: "Helius webhook management commands",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "helius-api-key",
				EnvVars: []string{"HELIUS_API_KEY"},
				Usage:   "Helius API key",
			},
			&cli.StringFlag{
				Name:    "helius-webhook-url",
				EnvVars: []string{"HELIUS_WEBHOOK_URL"},
				Usage:   "Public callback URL the webhook should deliver to (used to identify our webhook among others)",
			},
			&cli.StringFlag{
				Name:    "helius-webhook-auth-token",
				EnvVars: []string{"HELIUS_WEBHOOK_AUTH_TOKEN"},
				Usage:   "Auth header value Helius sends with each delivery (required for sync)",
			},
		},
		Subcommands: []*cli.Command{
			heliusListCommand(),
			heliusShowCommand(),
			heliusDiffCommand(),
			heliusSyncCommand(),
		},
	}
}

// quietLogger returns a slog.Logger that discards output, used for CLI helius client
// calls where we don't want chatty info-level lines on stderr.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// heliusClientFromCtx constructs a Helius client from CLI flags. Auth header is optional
// for read operations but required for sync.
func heliusClientFromCtx(c *cli.Context, requireAuth bool) (*helius.Client, error) {
	apiKey := c.String("helius-api-key")
	if apiKey == "" {
		return nil, fmt.Errorf("helius-api-key is required (set HELIUS_API_KEY)")
	}
	webhookURL := c.String("helius-webhook-url")
	authHeader := c.String("helius-webhook-auth-token")
	if requireAuth && authHeader == "" {
		return nil, fmt.Errorf("helius-webhook-auth-token is required for this operation (set HELIUS_WEBHOOK_AUTH_TOKEN)")
	}
	return helius.NewClient(apiKey, webhookURL, authHeader, quietLogger()), nil
}

func heliusListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List all webhooks for this Helius API key",
		Action: func(c *cli.Context) error {
			client, err := heliusClientFromCtx(c, false)
			if err != nil {
				return err
			}
			webhooks, err := client.ListWebhooks(context.Background())
			if err != nil {
				return fmt.Errorf("failed to list webhooks: %w", err)
			}
			if c.Bool("json") {
				return outputJSON(webhooks)
			}
			if len(webhooks) == 0 {
				fmt.Fprintln(os.Stderr, "no webhooks configured")
				return nil
			}
			for _, wh := range webhooks {
				fmt.Printf("WebhookID:   %s\n", wh.WebhookID)
				fmt.Printf("WebhookURL:  %s\n", wh.WebhookURL)
				fmt.Printf("Type:        %s\n", wh.WebhookType)
				fmt.Printf("TxnTypes:    %v\n", wh.TransactionTypes)
				fmt.Printf("Addresses:   %d\n", len(wh.AccountAddresses))
				fmt.Println("---")
			}
			fmt.Fprintf(os.Stderr, "Total: %d webhook(s)\n", len(webhooks))
			return nil
		},
	}
}

func heliusShowCommand() *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Show full details for a webhook (including all monitored addresses)",
		ArgsUsage: "[<webhook-id>]",
		Description: `If no webhook-id is provided, the webhook whose URL matches --helius-webhook-url is shown.
This is the same webhook the server manages.`,
		Action: func(c *cli.Context) error {
			client, err := heliusClientFromCtx(c, false)
			if err != nil {
				return err
			}
			ctx := context.Background()

			webhookID := c.Args().First()
			if webhookID == "" {
				webhookURL := c.String("helius-webhook-url")
				if webhookURL == "" {
					return fmt.Errorf("either pass a webhook-id argument or set --helius-webhook-url")
				}
				webhooks, err := client.ListWebhooks(ctx)
				if err != nil {
					return fmt.Errorf("failed to list webhooks: %w", err)
				}
				for _, wh := range webhooks {
					if wh.WebhookURL == webhookURL {
						webhookID = wh.WebhookID
						break
					}
				}
				if webhookID == "" {
					return fmt.Errorf("no webhook found matching URL %q", webhookURL)
				}
			}

			wh, err := client.GetWebhook(ctx, webhookID)
			if err != nil {
				return fmt.Errorf("failed to get webhook: %w", err)
			}
			if c.Bool("json") {
				return outputJSON(wh)
			}
			fmt.Printf("WebhookID:   %s\n", wh.WebhookID)
			fmt.Printf("WebhookURL:  %s\n", wh.WebhookURL)
			fmt.Printf("Type:        %s\n", wh.WebhookType)
			fmt.Printf("TxnTypes:    %v\n", wh.TransactionTypes)
			fmt.Printf("Addresses:   %d\n", len(wh.AccountAddresses))
			sorted := append([]string(nil), wh.AccountAddresses...)
			sort.Strings(sorted)
			for _, addr := range sorted {
				fmt.Println(addr)
			}
			return nil
		},
	}
}

// desiredAddressesFromDB returns the set of addresses the Helius webhook should monitor,
// derived from the active wallets in the DB. SOL wallets contribute their owner address,
// SPL token wallets contribute their associated token account (ATA).
func desiredAddressesFromDB(c *cli.Context) ([]string, error) {
	store, closer, err := getStore(c)
	if err != nil {
		return nil, err
	}
	defer closer()

	wallets, err := store.ListActiveWallets(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list active wallets: %w", err)
	}
	addrs := make([]string, 0, len(wallets))
	for _, w := range wallets {
		switch {
		case w.AssetType == "sol":
			addrs = append(addrs, w.Address)
		case w.AssociatedTokenAddress != nil && *w.AssociatedTokenAddress != "":
			addrs = append(addrs, *w.AssociatedTokenAddress)
		default:
			fmt.Fprintf(os.Stderr, "warning: skipping wallet %s (asset=%s) — no monitorable address\n", w.Address, w.AssetType)
		}
	}
	return addrs, nil
}

// resolveOurWebhookID locates the webhook whose URL matches the configured webhook URL.
func resolveOurWebhookID(ctx context.Context, client *helius.Client, webhookURL string) (string, error) {
	if webhookURL == "" {
		return "", fmt.Errorf("--helius-webhook-url is required to identify our webhook")
	}
	webhooks, err := client.ListWebhooks(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list webhooks: %w", err)
	}
	for _, wh := range webhooks {
		if wh.WebhookURL == webhookURL {
			return wh.WebhookID, nil
		}
	}
	return "", fmt.Errorf("no webhook found matching URL %q", webhookURL)
}

func heliusDiffCommand() *cli.Command {
	return &cli.Command{
		Name:  "diff",
		Usage: "Compare DB active wallets against the Helius webhook's monitored address list",
		Description: `Shows three groups:
  - missing: addresses in the DB but NOT in the webhook (Helius will MISS transactions for these)
  - extra:   addresses in the webhook but NOT in the DB (no harm, just unused capacity)
  - matched: addresses in both
Exits non-zero if there are missing addresses (so it's safe to use as a deploy precondition).`,
		Action: func(c *cli.Context) error {
			ctx := context.Background()

			client, err := heliusClientFromCtx(c, false)
			if err != nil {
				return err
			}
			webhookID, err := resolveOurWebhookID(ctx, client, c.String("helius-webhook-url"))
			if err != nil {
				return err
			}
			wh, err := client.GetWebhook(ctx, webhookID)
			if err != nil {
				return fmt.Errorf("failed to get webhook: %w", err)
			}

			desired, err := desiredAddressesFromDB(c)
			if err != nil {
				return err
			}

			webhookSet := make(map[string]bool, len(wh.AccountAddresses))
			for _, a := range wh.AccountAddresses {
				webhookSet[a] = true
			}
			dbSet := make(map[string]bool, len(desired))
			for _, a := range desired {
				dbSet[a] = true
			}

			var missing, extra, matched []string
			for a := range dbSet {
				if webhookSet[a] {
					matched = append(matched, a)
				} else {
					missing = append(missing, a)
				}
			}
			for a := range webhookSet {
				if !dbSet[a] {
					extra = append(extra, a)
				}
			}
			sort.Strings(missing)
			sort.Strings(extra)
			sort.Strings(matched)

			if c.Bool("json") {
				return outputJSON(map[string]interface{}{
					"webhook_id": webhookID,
					"db_count":   len(dbSet),
					"hook_count": len(webhookSet),
					"matched":    matched,
					"missing":    missing,
					"extra":      extra,
				})
			}

			fmt.Fprintf(os.Stderr, "webhook:    %s (%s)\n", webhookID, wh.WebhookURL)
			fmt.Fprintf(os.Stderr, "db active:  %d wallet(s) -> monitorable addresses\n", len(dbSet))
			fmt.Fprintf(os.Stderr, "on webhook: %d address(es)\n", len(webhookSet))
			fmt.Fprintf(os.Stderr, "matched:    %d\n", len(matched))
			fmt.Fprintf(os.Stderr, "missing:    %d  (in DB, NOT on webhook)\n", len(missing))
			fmt.Fprintf(os.Stderr, "extra:      %d  (on webhook, NOT in DB)\n\n", len(extra))

			if len(missing) > 0 {
				fmt.Fprintln(os.Stderr, "MISSING (webhook will not deliver these):")
				for _, a := range missing {
					fmt.Println(a)
				}
				fmt.Fprintln(os.Stderr)
			}
			if len(extra) > 0 {
				fmt.Fprintln(os.Stderr, "EXTRA (webhook monitors but DB doesn't care):")
				for _, a := range extra {
					fmt.Fprintln(os.Stderr, "  "+a)
				}
			}

			if len(missing) > 0 {
				return cli.Exit(fmt.Sprintf("%d address(es) missing from webhook — run 'forohtoo helius sync' or restart the server", len(missing)), 1)
			}
			fmt.Fprintln(os.Stderr, "OK: webhook is in sync with DB")
			return nil
		},
	}
}

func heliusSyncCommand() *cli.Command {
	return &cli.Command{
		Name:  "sync",
		Usage: "Push the DB's active wallet address list to the Helius webhook (idempotent)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show what would change without calling the Helius update API",
			},
		},
		Action: func(c *cli.Context) error {
			ctx := context.Background()

			// Read ops don't need auth, but UpdateWebhookAddresses sends authHeader as part of the body,
			// so require it for non-dry-run.
			client, err := heliusClientFromCtx(c, !c.Bool("dry-run"))
			if err != nil {
				return err
			}
			webhookID, err := resolveOurWebhookID(ctx, client, c.String("helius-webhook-url"))
			if err != nil {
				return err
			}
			// Prime the cached webhook ID for SyncAddresses/AddAddress.
			if err := client.EnsureWebhooks(ctx); err != nil {
				return fmt.Errorf("failed to ensure webhook: %w", err)
			}
			if client.WebhookID() != webhookID {
				return fmt.Errorf("webhook ID mismatch after EnsureWebhooks: got %q, expected %q", client.WebhookID(), webhookID)
			}

			desired, err := desiredAddressesFromDB(c)
			if err != nil {
				return err
			}

			if c.Bool("dry-run") {
				wh, err := client.GetWebhook(ctx, webhookID)
				if err != nil {
					return fmt.Errorf("failed to get webhook: %w", err)
				}
				fmt.Fprintf(os.Stderr, "DRY RUN: would set %d address(es) on webhook %s (currently %d)\n",
					len(desired), webhookID, len(wh.AccountAddresses))
				return nil
			}

			if err := client.SyncAddresses(ctx, desired); err != nil {
				return fmt.Errorf("sync failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "synced %d address(es) to webhook %s\n", len(desired), webhookID)
			return nil
		},
	}
}
