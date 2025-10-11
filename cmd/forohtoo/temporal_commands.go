package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/urfave/cli/v2"
	"go.temporal.io/sdk/client"
)

func listSchedulesCommand() *cli.Command {
	return &cli.Command{
		Name:    "list-schedules",
		Usage:   "List all Temporal schedules",
		Aliases: []string{"ls"},
		Action: func(c *cli.Context) error {
			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			iter, err := temporalClient.ScheduleClient().List(ctx, client.ScheduleListOptions{
				PageSize: 100,
			})
			if err != nil {
				return fmt.Errorf("failed to list schedules: %w", err)
			}

			// Pretty table output
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SCHEDULE ID")
			count := 0
			for iter.HasNext() {
				schedule, err := iter.Next()
				if err != nil {
					return fmt.Errorf("failed to iterate schedules: %w", err)
				}
				fmt.Fprintf(w, "%s\n", schedule.ID)
				count++
			}
			w.Flush()

			fmt.Fprintf(os.Stderr, "\nTotal: %d schedules\n", count)
			return nil
		},
	}
}

func describeScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "describe-schedule",
		Usage:     "Describe a Temporal schedule",
		Aliases:   []string{"desc"},
		ArgsUsage: "<schedule-id>",
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("requires exactly one argument: schedule ID")
			}

			scheduleID := c.Args().First()
			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
			desc, err := handle.Describe(ctx)
			if err != nil {
				return fmt.Errorf("failed to describe schedule: %w", err)
			}

			// Pretty output
			fmt.Printf("Schedule ID:    %s\n", scheduleID)
			fmt.Printf("State Note:     %s\n", desc.Schedule.State.Note)
			fmt.Printf("Paused:         %v\n", desc.Schedule.State.Paused)

			if action := desc.Schedule.Action; action != nil {
				if wa, ok := action.(*client.ScheduleWorkflowAction); ok {
					fmt.Printf("\nWorkflow:\n")
					fmt.Printf("  Workflow:     %s\n", wa.Workflow)
					fmt.Printf("  Task Queue:   %s\n", wa.TaskQueue)
					fmt.Printf("  Args:         %v\n", wa.Args)
				}
			}

			if len(desc.Schedule.Spec.Intervals) > 0 {
				fmt.Printf("\nSchedule Spec:\n")
				for i, interval := range desc.Schedule.Spec.Intervals {
					fmt.Printf("  Interval %d:   Every %v\n", i+1, interval.Every)
				}
			}

			fmt.Printf("\nRecent Actions: %d\n", len(desc.Info.RecentActions))
			if len(desc.Info.RecentActions) > 0 {
				lastAction := desc.Info.RecentActions[len(desc.Info.RecentActions)-1]
				fmt.Printf("Last Action:  %s\n", lastAction.ActualTime.Format(time.RFC3339))
			}

			return nil
		},
	}
}

func listWorkflowsCommand() *cli.Command {
	return &cli.Command{
		Name:    "list-workflows",
		Usage:   "List workflow executions (requires Temporal connection)",
		Aliases: []string{"wf"},
		Action: func(c *cli.Context) error {
			return fmt.Errorf("workflow listing not yet implemented - use Temporal UI or CLI")
		},
	}
}

func pauseScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "pause-schedule",
		Usage:     "Pause a Temporal schedule",
		ArgsUsage: "<schedule-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "note",
				Usage: "Note explaining why schedule is paused",
				Value: "Paused via forohtoo CLI",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("requires exactly one argument: schedule ID")
			}

			scheduleID := c.Args().First()
			note := c.String("note")

			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
			err = handle.Pause(ctx, client.SchedulePauseOptions{
				Note: note,
			})
			if err != nil {
				return fmt.Errorf("failed to pause schedule: %w", err)
			}

			fmt.Printf("✓ Schedule paused: %s\n", scheduleID)
			if note != "" {
				fmt.Printf("  Note: %s\n", note)
			}
			return nil
		},
	}
}

func resumeScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "resume-schedule",
		Usage:     "Resume a paused Temporal schedule",
		ArgsUsage: "<schedule-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "note",
				Usage: "Note explaining why schedule is resumed",
				Value: "Resumed via forohtoo CLI",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("requires exactly one argument: schedule ID")
			}

			scheduleID := c.Args().First()
			note := c.String("note")

			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
			err = handle.Unpause(ctx, client.ScheduleUnpauseOptions{
				Note: note,
			})
			if err != nil {
				return fmt.Errorf("failed to resume schedule: %w", err)
			}

			fmt.Printf("✓ Schedule resumed: %s\n", scheduleID)
			if note != "" {
				fmt.Printf("  Note: %s\n", note)
			}
			return nil
		},
	}
}

func deleteScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete-schedule",
		Usage:     "Delete a Temporal schedule (use for orphaned schedules)",
		ArgsUsage: "<schedule-id>",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Skip confirmation prompt",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 1 {
				return fmt.Errorf("requires exactly one argument: schedule ID")
			}

			scheduleID := c.Args().First()

			// Confirm deletion unless --force
			if !c.Bool("force") {
				fmt.Printf("Are you sure you want to delete schedule %s? (yes/no): ", scheduleID)
				var response string
				fmt.Scanln(&response)
				if response != "yes" {
					fmt.Println("Cancelled")
					return nil
				}
			}

			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
			err = handle.Delete(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete schedule: %w", err)
			}

			fmt.Printf("✓ Schedule deleted: %s\n", scheduleID)
			return nil
		},
	}
}

func createScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "create-schedule",
		Usage:     "Manually create a Temporal schedule for a wallet",
		ArgsUsage: "<wallet-address> <poll-interval>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "task-queue",
				Usage: "Task queue name",
				Value: "forohtoo-wallet-polling",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 2 {
				return fmt.Errorf("requires exactly two arguments: wallet-address poll-interval")
			}

			address := c.Args().Get(0)
			intervalStr := c.Args().Get(1)
			taskQueue := c.String("task-queue")

			// Parse interval
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid poll-interval: %w", err)
			}

			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			scheduleID := "poll-wallet-" + address

			// Create schedule spec
			scheduleSpec := client.ScheduleSpec{
				Intervals: []client.ScheduleIntervalSpec{
					{
						Every: interval,
					},
				},
			}

			// Create workflow action with proper input struct
			workflowAction := client.ScheduleWorkflowAction{
				ID:        fmt.Sprintf("poll-wallet-%s-${ScheduledTime}", address),
				Workflow:  "PollWalletWorkflow",
				TaskQueue: taskQueue,
				Args: []interface{}{map[string]interface{}{
					"address": address,
				}},
			}

			// Create the schedule
			_, err = temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
				ID:     scheduleID,
				Spec:   scheduleSpec,
				Action: &workflowAction,
				Memo: map[string]interface{}{
					"wallet_address": address,
					"created_by":     "forohtoo-cli",
				},
			})

			if err != nil {
				return fmt.Errorf("failed to create schedule: %w", err)
			}

			fmt.Printf("✓ Schedule created: %s\n", scheduleID)
			fmt.Printf("  Wallet: %s\n", address)
			fmt.Printf("  Interval: %v\n", interval)
			fmt.Printf("  Task Queue: %s\n", taskQueue)
			return nil
		},
	}
}

func recreateScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:      "recreate-schedule",
		Usage:     "Delete and recreate a Temporal schedule for a wallet (useful after code updates)",
		ArgsUsage: "<wallet-address> <poll-interval>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "task-queue",
				Usage: "Task queue name",
				Value: "forohtoo-wallet-polling",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() != 2 {
				return fmt.Errorf("requires exactly two arguments: wallet-address poll-interval")
			}

			address := c.Args().Get(0)
			intervalStr := c.Args().Get(1)
			taskQueue := c.String("task-queue")

			// Parse interval
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return fmt.Errorf("invalid poll-interval: %w", err)
			}

			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()
			scheduleID := "poll-wallet-" + address

			// Try to delete existing schedule (ignore error if it doesn't exist)
			fmt.Printf("Deleting existing schedule %s...\n", scheduleID)
			handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
			err = handle.Delete(ctx)
			if err != nil {
				fmt.Printf("  Note: Schedule may not exist (this is OK): %v\n", err)
			} else {
				fmt.Printf("  ✓ Existing schedule deleted\n")
			}

			// Create schedule spec
			scheduleSpec := client.ScheduleSpec{
				Intervals: []client.ScheduleIntervalSpec{
					{
						Every: interval,
					},
				},
			}

			// Create workflow action with proper input struct
			workflowAction := client.ScheduleWorkflowAction{
				ID:        fmt.Sprintf("poll-wallet-%s-${ScheduledTime}", address),
				Workflow:  "PollWalletWorkflow",
				TaskQueue: taskQueue,
				Args: []interface{}{map[string]interface{}{
					"address": address,
				}},
			}

			// Create the schedule
			fmt.Printf("\nCreating new schedule...\n")
			_, err = temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
				ID:     scheduleID,
				Spec:   scheduleSpec,
				Action: &workflowAction,
				Memo: map[string]interface{}{
					"wallet_address": address,
					"created_by":     "forohtoo-cli-recreate",
					"recreated_at":   time.Now().Format(time.RFC3339),
				},
			})

			if err != nil {
				return fmt.Errorf("failed to create schedule: %w", err)
			}

			fmt.Printf("✓ Schedule recreated successfully!\n")
			fmt.Printf("  Schedule ID: %s\n", scheduleID)
			fmt.Printf("  Wallet: %s\n", address)
			fmt.Printf("  Interval: %v\n", interval)
			fmt.Printf("  Task Queue: %s\n", taskQueue)
			return nil
		},
	}
}

func reconcileCommand() *cli.Command {
	return &cli.Command{
		Name:  "reconcile",
		Usage: "Check for inconsistencies between database and Temporal schedules",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "fix",
				Usage: "Automatically fix inconsistencies (creates missing schedules, deletes orphaned ones)",
			},
			&cli.StringFlag{
				Name:  "task-queue",
				Usage: "Task queue name for created schedules",
				Value: "forohtoo-wallet-polling",
			},
		},
		Action: func(c *cli.Context) error {
			// Get database connection
			store, closer, err := getStore(c)
			if err != nil {
				return err
			}
			defer closer()

			// Get Temporal client
			temporalClient, err := getTemporalClient(c)
			if err != nil {
				return err
			}
			defer temporalClient.Close()

			ctx := context.Background()

			// Get all wallets from DB
			wallets, err := store.ListWallets(ctx)
			if err != nil {
				return fmt.Errorf("failed to list wallets: %w", err)
			}

			// Get all schedules from Temporal
			iter, err := temporalClient.ScheduleClient().List(ctx, client.ScheduleListOptions{
				PageSize: 1000,
			})
			if err != nil {
				return fmt.Errorf("failed to list schedules: %w", err)
			}

			schedules := make(map[string]bool)
			for iter.HasNext() {
				schedule, err := iter.Next()
				if err != nil {
					return fmt.Errorf("failed to iterate schedules: %w", err)
				}
				schedules[schedule.ID] = true
			}

			// Check for inconsistencies
			var missingSchedules []string
			var orphanedSchedules []string

			// Find wallets without schedules
			for _, wallet := range wallets {
				if wallet.Status != "active" {
					continue // Skip non-active wallets
				}
				scheduleID := "poll-wallet-" + wallet.Address
				if !schedules[scheduleID] {
					missingSchedules = append(missingSchedules, wallet.Address)
				}
			}

			// Find schedules without wallets
			walletsMap := make(map[string]bool)
			for _, wallet := range wallets {
				walletsMap[wallet.Address] = true
			}

			for scheduleID := range schedules {
				// Extract wallet address from schedule ID
				if len(scheduleID) > 12 && scheduleID[:12] == "poll-wallet-" {
					address := scheduleID[12:]
					if !walletsMap[address] {
						orphanedSchedules = append(orphanedSchedules, scheduleID)
					}
				}
			}

			// Report findings
			fmt.Printf("Reconciliation Report:\n")
			fmt.Printf("  Wallets in DB: %d\n", len(wallets))
			fmt.Printf("  Schedules in Temporal: %d\n", len(schedules))
			fmt.Printf("\n")

			if len(missingSchedules) > 0 {
				fmt.Printf("⚠ Wallets missing schedules (%d):\n", len(missingSchedules))
				for _, addr := range missingSchedules {
					fmt.Printf("  - %s\n", addr)
				}
			} else {
				fmt.Printf("✓ All active wallets have schedules\n")
			}

			if len(orphanedSchedules) > 0 {
				fmt.Printf("\n⚠ Orphaned schedules (%d):\n", len(orphanedSchedules))
				for _, schedID := range orphanedSchedules {
					fmt.Printf("  - %s\n", schedID)
				}
			} else {
				fmt.Printf("✓ No orphaned schedules\n")
			}

			// Fix if requested
			if c.Bool("fix") && (len(missingSchedules) > 0 || len(orphanedSchedules) > 0) {
				fmt.Printf("\nFixing inconsistencies...\n")

				// Create missing schedules
				for _, addr := range missingSchedules {
					// Get wallet to get poll interval
					wallet, err := store.GetWallet(ctx, addr)
					if err != nil {
						fmt.Printf("  ✗ Failed to get wallet %s: %v\n", addr, err)
						continue
					}

					scheduleID := "poll-wallet-" + addr
					scheduleSpec := client.ScheduleSpec{
						Intervals: []client.ScheduleIntervalSpec{
							{
								Every: wallet.PollInterval,
							},
						},
					}

					workflowAction := client.ScheduleWorkflowAction{
						ID:        fmt.Sprintf("poll-wallet-%s-${ScheduledTime}", addr),
						Workflow:  "PollWalletWorkflow",
						TaskQueue: c.String("task-queue"),
						Args: []interface{}{map[string]interface{}{
							"address": addr,
						}},
					}

					_, err = temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
						ID:     scheduleID,
						Spec:   scheduleSpec,
						Action: &workflowAction,
						Memo: map[string]interface{}{
							"wallet_address": addr,
							"created_by":     "forohtoo-cli-reconcile",
						},
					})

					if err != nil {
						fmt.Printf("  ✗ Failed to create schedule for %s: %v\n", addr, err)
					} else {
						fmt.Printf("  ✓ Created schedule for %s\n", addr)
					}
				}

				// Delete orphaned schedules
				for _, schedID := range orphanedSchedules {
					handle := temporalClient.ScheduleClient().GetHandle(ctx, schedID)
					err := handle.Delete(ctx)
					if err != nil {
						fmt.Printf("  ✗ Failed to delete schedule %s: %v\n", schedID, err)
					} else {
						fmt.Printf("  ✓ Deleted orphaned schedule %s\n", schedID)
					}
				}

				fmt.Printf("\nReconciliation complete!\n")
			} else if len(missingSchedules) > 0 || len(orphanedSchedules) > 0 {
				fmt.Printf("\nTo fix these issues, run: forohtoo temporal reconcile --fix\n")
			}

			return nil
		},
	}
}

// Helper function to connect to Temporal
func getTemporalClient(c *cli.Context) (client.Client, error) {
	// Try to get from parent context first (for global flags)
	host := c.String("temporal-host")
	if host == "" && c.App != nil {
		// Try environment variable directly if flag not found
		host = os.Getenv("TEMPORAL_HOST")
	}
	if host == "" {
		host = "localhost:7233" // Default value
	}

	namespace := c.String("temporal-namespace")
	if namespace == "" && c.App != nil {
		// Try environment variable directly if flag not found
		namespace = os.Getenv("TEMPORAL_NAMESPACE")
	}
	if namespace == "" {
		namespace = "default" // Default value
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort:  host,
		Namespace: namespace,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}

	return temporalClient, nil
}
