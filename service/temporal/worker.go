package temporal

import (
	"fmt"
	"log/slog"

	forohtoo "github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/helius"
	"github.com/brojonat/forohtoo/service/metrics"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// WorkerConfig contains configuration for the Temporal worker.
type WorkerConfig struct {
	TemporalHost      string
	TemporalNamespace string
	TaskQueue         string

	Store          StoreInterface
	HeliusClient   *helius.Client
	ForohtooClient *forohtoo.Client
	Metrics        *metrics.Metrics
	Logger         *slog.Logger
}

// Worker wraps a Temporal worker and provides lifecycle management.
type Worker struct {
	client client.Client
	worker worker.Worker
	logger *slog.Logger
}

// NewWorker creates and configures a new Temporal worker for payment-gated
// registration workflows. There is no polling worker anymore — transaction
// ingestion is handled by Helius webhooks directly into the HTTP server.
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

	c, err := client.Dial(client.Options{
		HostPort:  config.TemporalHost,
		Namespace: config.TemporalNamespace,
		Logger:    newTemporalLogger(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to temporal: %w", err)
	}

	w := worker.New(c, config.TaskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize:     10,
		MaxConcurrentWorkflowTaskExecutionSize: 10,
	})

	w.RegisterWorkflow(PaymentGatedRegistrationWorkflow)

	activities := NewActivities(
		config.Store,
		config.HeliusClient,
		config.ForohtooClient,
		config.Metrics,
		logger,
	)
	w.RegisterActivity(activities.AwaitPayment)
	w.RegisterActivity(activities.RegisterWallet)

	logger.Info("registered payment-gateway workflow and activities")

	return &Worker{
		client: c,
		worker: w,
		logger: logger,
	}, nil
}

// Start begins processing workflows and activities. Non-blocking.
func (w *Worker) Start() error {
	w.logger.Info("starting temporal worker")
	if err := w.worker.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}
	return nil
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	w.logger.Info("stopping temporal worker")
	w.worker.Stop()
	w.client.Close()
	w.logger.Info("temporal worker stopped")
}
