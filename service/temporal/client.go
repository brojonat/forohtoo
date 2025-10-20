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

// CreateWalletSchedule creates a new Temporal schedule for polling a wallet.
func (c *Client) CreateWalletSchedule(ctx context.Context, address string, network string, interval time.Duration) error {
	id := scheduleID(address, network)

	c.logger.Debug("creating wallet schedule",
		"address", address,
		"network", network,
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

	// Create workflow action - this will execute the PollWalletWorkflow
	workflowAction := client.ScheduleWorkflowAction{
		ID:        fmt.Sprintf("poll-wallet-%s-%s", network, address),
		Workflow:  "PollWalletWorkflow",
		TaskQueue: c.taskQueue,
		Args:      []interface{}{PollWalletInput{Address: address, Network: network}},
	}

	// Create the schedule
	_, err := c.client.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     id,
		Spec:   scheduleSpec,
		Action: &workflowAction,
		Memo: map[string]interface{}{
			"wallet_address": address,
			"network":        network,
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

	c.logger.Info("wallet schedule created",
		"address", address,
		"schedule_id", id,
		"interval", interval,
	)

	return nil
}

// DeleteWalletSchedule deletes the Temporal schedule for a wallet.
func (c *Client) DeleteWalletSchedule(ctx context.Context, address string, network string) error {
	id := scheduleID(address, network)

	c.logger.Debug("deleting wallet schedule",
		"address", address,
		"network", network,
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

	c.logger.Info("wallet schedule deleted",
		"address", address,
		"schedule_id", id,
	)

	return nil
}

// Close closes the Temporal client connection.
func (c *Client) Close() {
	c.logger.Info("closing temporal client")
	c.client.Close()
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
