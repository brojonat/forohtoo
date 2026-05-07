package temporal

import (
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/client"
)

// Client is a thin wrapper around the Temporal SDK client used to drive the
// payment-gated registration workflow from the HTTP server.
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

	return &Client{
		client:    c,
		taskQueue: taskQueue,
		logger:    logger,
	}, nil
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

// temporalLogger adapts slog.Logger to Temporal's logger interface.
type temporalLogger struct {
	logger *slog.Logger
}

func newTemporalLogger(logger *slog.Logger) *temporalLogger {
	return &temporalLogger{logger: logger}
}

func (l *temporalLogger) Debug(msg string, keyvals ...interface{}) { l.logger.Debug(msg, keyvals...) }
func (l *temporalLogger) Info(msg string, keyvals ...interface{})  { l.logger.Info(msg, keyvals...) }
func (l *temporalLogger) Warn(msg string, keyvals ...interface{})  { l.logger.Warn(msg, keyvals...) }
func (l *temporalLogger) Error(msg string, keyvals ...interface{}) { l.logger.Error(msg, keyvals...) }
