package temporal

import (
	"fmt"
	"log/slog"

	"github.com/brojonat/forohtoo/service/metrics"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// WorkerConfig contains configuration for the Temporal worker.
type WorkerConfig struct {
	// Temporal connection settings
	TemporalHost      string
	TemporalNamespace string
	TaskQueue         string

	// Dependencies
	Store        StoreInterface
	SolanaClient SolanaClientInterface
	Publisher    PublisherInterface
	Metrics      *metrics.Metrics // Optional: if nil, no metrics will be recorded
	Logger       *slog.Logger
}

// Worker wraps a Temporal worker and provides lifecycle management.
type Worker struct {
	client         client.Client
	worker         worker.Worker
	logger         *slog.Logger
}

// NewWorker creates and configures a new Temporal worker.
// The worker will process workflows and activities on the configured task queue.
func NewWorker(config WorkerConfig) (*Worker, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	logger := config.Logger.With("component", "temporal_worker")

	logger.Info("creating temporal worker",
		"host", config.TemporalHost,
		"namespace", config.TemporalNamespace,
		"task_queue", config.TaskQueue,
	)

	// Connect to Temporal
	c, err := client.Dial(client.Options{
		HostPort:  config.TemporalHost,
		Namespace: config.TemporalNamespace,
		Logger:    newTemporalLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to temporal: %w", err)
	}

	// Create worker
	w := worker.New(c, config.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize: 10,
		MaxConcurrentWorkflowTaskExecutionSize: 10,
	})

	// Register workflow
	w.RegisterWorkflow(PollWalletWorkflow)
	logger.Info("registered workflow", "name", "PollWalletWorkflow")

	// Create activities instance with dependencies
	activities := NewActivities(
		config.Store,
		config.SolanaClient,
		config.Publisher,
		config.Metrics,
		logger,
	)

	// Register activities
	// Activities are registered by name, matching the ExecuteActivity calls in the workflow
	w.RegisterActivity(activities.GetExistingTransactionSignatures)
	w.RegisterActivity(activities.PollSolana)
	w.RegisterActivity(activities.WriteTransactions)

	logger.Info("registered activities",
		"activities", []string{"GetExistingTransactionSignatures", "PollSolana", "WriteTransactions"},
	)

	return &Worker{
		client: c,
		worker: w,
		logger: logger,
	}, nil
}

// Start begins processing workflows and activities.
// This method blocks until Stop is called or an error occurs.
func (w *Worker) Start() error {
	w.logger.Info("starting temporal worker")
	err := w.worker.Run(worker.InterruptCh())
	if err != nil {
		w.logger.Error("worker stopped with error", "error", err)
		return fmt.Errorf("worker stopped with error: %w", err)
	}
	w.logger.Info("worker stopped gracefully")
	return nil
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	w.logger.Info("stopping temporal worker")
	w.worker.Stop()
	w.client.Close()
	w.logger.Info("temporal worker stopped")
}
