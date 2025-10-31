package temporal

import (
	"fmt"
	"log/slog"

	forohtoo "github.com/brojonat/forohtoo/client"
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

	// Solana configuration
	USDCMainnetMintAddress string // SPL token mint address for USDC on mainnet
	USDCDevnetMintAddress  string // SPL token mint address for USDC on devnet

	// Dependencies
	Store          StoreInterface
	MainnetClient  SolanaClientInterface
	DevnetClient   SolanaClientInterface
	Publisher      PublisherInterface
	ForohtooClient *forohtoo.Client // Forohtoo client for awaiting payment transactions
	TemporalClient *Client          // Temporal client for creating wallet schedules
	Metrics        *metrics.Metrics // Optional: if nil, no metrics will be recorded
	Logger         *slog.Logger
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

	// Set the USDC mint addresses for workflow use
	USDCMainnetMintAddress = config.USDCMainnetMintAddress
	USDCDevnetMintAddress = config.USDCDevnetMintAddress

	if USDCMainnetMintAddress != "" {
		logger.Info("USDC ATA polling enabled for mainnet", "usdc_mint", USDCMainnetMintAddress)
	} else {
		logger.Warn("USDC ATA polling disabled for mainnet (no USDC_MAINNET_MINT_ADDRESS configured)")
	}

	if USDCDevnetMintAddress != "" {
		logger.Info("USDC ATA polling enabled for devnet", "usdc_mint", USDCDevnetMintAddress)
	} else {
		logger.Warn("USDC ATA polling disabled for devnet (no USDC_DEVNET_MINT_ADDRESS configured)")
	}

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

	// Register workflows
	w.RegisterWorkflow(PollWalletWorkflow)
	w.RegisterWorkflow(PaymentGatedRegistrationWorkflow)
	logger.Info("registered workflows",
		"workflows", []string{"PollWalletWorkflow", "PaymentGatedRegistrationWorkflow"},
	)

	// Create activities instance with dependencies
	activities := NewActivities(
		config.Store,
		config.MainnetClient,
		config.DevnetClient,
		config.Publisher,
		config.ForohtooClient,
		config.TemporalClient,
		config.Metrics,
		logger,
	)

	// Register activities
	// Activities are registered by name, matching the ExecuteActivity calls in the workflow
	w.RegisterActivity(activities.GetExistingTransactionSignatures)
	w.RegisterActivity(activities.PollSolana)
	w.RegisterActivity(activities.WriteTransactions)
	w.RegisterActivity(activities.AwaitPayment)
	w.RegisterActivity(activities.RegisterWallet)

	logger.Info("registered activities",
		"activities", []string{
			"GetExistingTransactionSignatures",
			"PollSolana",
			"WriteTransactions",
			"AwaitPayment",
			"RegisterWallet",
		},
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
