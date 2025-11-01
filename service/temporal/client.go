package temporal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
)

// Client is a production implementation of Scheduler that talks to Temporal.
type Client struct {
	client    client.Client
	taskQueue string
	logger    *slog.Logger
}

// NewClient creates a new Temporal client.
func NewClient(host, namespace, taskQueue string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("connecting to temporal",
		"host", host,
		"namespace", namespace,
		"task_queue", taskQueue,
	)

	c, err := client.Dial(client.Options{
		HostPort:  host,
		Namespace: namespace,
		Logger:    newTemporalLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Temporal: %w", err)
	}

	logger.Info("connected to temporal successfully")

	return &Client{
		client:    c,
		taskQueue: taskQueue,
		logger:    logger,
	}, nil
}

// CreateWalletAssetSchedule creates a new Temporal schedule for polling a wallet asset.
func (c *Client) CreateWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string, ata *string, interval time.Duration) error {
	id := scheduleID(address, network, assetType, tokenMint)

	c.logger.Debug("creating wallet asset schedule",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"schedule_id", id,
		"interval", interval,
	)

	// Create schedule spec
	scheduleSpec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{
			{
				Every: interval,
			},
		},
	}

	// Determine poll address based on asset type
	var pollAddress string
	if assetType == "sol" {
		pollAddress = address
	} else {
		// For SPL tokens, poll the ATA
		if ata == nil {
			return fmt.Errorf("ATA is required for spl-token asset type")
		}
		pollAddress = *ata
	}

	// Create workflow action - this will execute the PollWalletWorkflow
	workflowAction := client.ScheduleWorkflowAction{
		ID:        fmt.Sprintf("poll-wallet-%s-%s-%s", network, address, pollAddress),
		Workflow:  "PollWalletWorkflow",
		TaskQueue: c.taskQueue,
		Args: []interface{}{PollWalletInput{
			WalletAddress:          address,
			Network:                network,
			AssetType:              assetType,
			TokenMint:              tokenMint,
			AssociatedTokenAddress: ata,
			PollAddress:            pollAddress,
		}},
	}

	// Create the schedule
	_, err := c.client.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     id,
		Spec:   scheduleSpec,
		Action: &workflowAction,
		Memo: map[string]interface{}{
			"wallet_address": address,
			"network":        network,
			"asset_type":     assetType,
			"token_mint":     tokenMint,
			"poll_address":   pollAddress,
			"created_by":     "forohtoo",
		},
	})

	if err != nil {
		c.logger.Error("failed to create schedule",
			"address", address,
			"schedule_id", id,
			"error", err,
		)
		return fmt.Errorf("failed to create schedule %q: %w", id, err)
	}

	c.logger.Info("wallet asset schedule created",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"poll_address", pollAddress,
		"schedule_id", id,
		"interval", interval,
	)

	return nil
}

// UpsertWalletAssetSchedule creates or updates a Temporal schedule for polling a wallet asset.
// If the schedule already exists, it updates the poll interval. Otherwise, it creates a new schedule.
func (c *Client) UpsertWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string, ata *string, interval time.Duration) error {
	id := scheduleID(address, network, assetType, tokenMint)

	c.logger.Debug("upserting wallet asset schedule",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"schedule_id", id,
		"interval", interval,
	)

	// Determine poll address based on asset type
	var pollAddress string
	if assetType == "sol" {
		pollAddress = address
	} else {
		// For SPL tokens, poll the ATA
		if ata == nil {
			return fmt.Errorf("ATA is required for spl-token asset type")
		}
		pollAddress = *ata
	}

	// Try to get existing schedule
	handle := c.client.ScheduleClient().GetHandle(ctx, id)
	desc, err := handle.Describe(ctx)

	if err != nil {
		// Schedule doesn't exist or error getting it - create new one
		c.logger.Debug("schedule not found, creating new one",
			"schedule_id", id,
			"error", err,
		)
		return c.CreateWalletAssetSchedule(ctx, address, network, assetType, tokenMint, ata, interval)
	}

	// Schedule exists - update the interval
	c.logger.Debug("schedule exists, updating interval",
		"schedule_id", id,
		"old_interval", desc.Schedule.Spec.Intervals[0].Every,
		"new_interval", interval,
	)

	// Update the schedule spec with new interval
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			// Update the interval
			input.Description.Schedule.Spec.Intervals = []client.ScheduleIntervalSpec{
				{Every: interval},
			}
			return &client.ScheduleUpdate{
				Schedule: &input.Description.Schedule,
			}, nil
		},
	})

	if err != nil {
		c.logger.Error("failed to update schedule",
			"address", address,
			"schedule_id", id,
			"error", err,
		)
		return fmt.Errorf("failed to update schedule %q: %w", id, err)
	}

	c.logger.Info("wallet asset schedule updated",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"poll_address", pollAddress,
		"schedule_id", id,
		"interval", interval,
	)

	return nil
}

// DeleteWalletAssetSchedule deletes the Temporal schedule for a wallet asset.
func (c *Client) DeleteWalletAssetSchedule(ctx context.Context, address string, network string, assetType string, tokenMint string) error {
	id := scheduleID(address, network, assetType, tokenMint)

	c.logger.Debug("deleting wallet asset schedule",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"schedule_id", id,
	)

	handle := c.client.ScheduleClient().GetHandle(ctx, id)
	if err := handle.Delete(ctx); err != nil {
		c.logger.Error("failed to delete schedule",
			"address", address,
			"schedule_id", id,
			"error", err,
		)
		return fmt.Errorf("failed to delete schedule %q: %w", id, err)
	}

	c.logger.Info("wallet asset schedule deleted",
		"address", address,
		"network", network,
		"asset_type", assetType,
		"token_mint", tokenMint,
		"schedule_id", id,
	)

	return nil
}

// SDKClient returns the underlying Temporal SDK client for direct workflow operations.
func (c *Client) SDKClient() client.Client {
	return c.client
}

// TaskQueue returns the configured task queue for this client.
func (c *Client) TaskQueue() string {
	return c.taskQueue
}

// Close closes the Temporal client connection.
func (c *Client) Close() {
	c.logger.Info("closing temporal client")
	c.client.Close()
}

// scheduleID generates a unique schedule ID for a wallet asset.
func scheduleID(address string, network string, assetType string, tokenMint string) string {
	return "poll-wallet-" + network + "-" + address + "-" + assetType + "-" + tokenMint
}

// temporalLogger adapts slog.Logger to Temporal's logger interface.
type temporalLogger struct {
	logger *slog.Logger
}

func newTemporalLogger(logger *slog.Logger) *temporalLogger {
	return &temporalLogger{logger: logger}
}

func (l *temporalLogger) Debug(msg string, keyvals ...interface{}) {
	l.logger.Debug(msg, keyvals...)
}

func (l *temporalLogger) Info(msg string, keyvals ...interface{}) {
	l.logger.Info(msg, keyvals...)
}

func (l *temporalLogger) Warn(msg string, keyvals ...interface{}) {
	l.logger.Warn(msg, keyvals...)
}

func (l *temporalLogger) Error(msg string, keyvals ...interface{}) {
	l.logger.Error(msg, keyvals...)
}
