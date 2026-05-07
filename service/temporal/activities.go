package temporal

import (
	"context"
	"log/slog"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/brojonat/forohtoo/service/helius"
	"github.com/brojonat/forohtoo/service/metrics"
)

// StoreInterface defines the database operations needed by activities.
type StoreInterface interface {
	UpsertWallet(context.Context, db.UpsertWalletParams) (*db.Wallet, error)
	DeleteWallet(context.Context, string, string, string, string) error
	GetWallet(context.Context, string, string, string, string) (*db.Wallet, error)
}

// HeliusClientInterface defines the Helius webhook operations needed by activities.
type HeliusClientInterface interface {
	AddAddress(ctx context.Context, address string) error
	RemoveAddress(ctx context.Context, address string) error
}

// Activities holds the dependencies needed by Temporal activities.
type Activities struct {
	store          StoreInterface
	heliusClient   HeliusClientInterface
	forohtooClient *client.Client
	metrics        *metrics.Metrics
	logger         *slog.Logger
}

// NewActivities creates a new Activities instance with explicit dependencies.
func NewActivities(
	store StoreInterface,
	heliusClient HeliusClientInterface,
	forohtooClient *client.Client,
	m *metrics.Metrics,
	logger *slog.Logger,
) *Activities {
	if logger == nil {
		logger = slog.Default()
	}
	return &Activities{
		store:          store,
		heliusClient:   heliusClient,
		forohtooClient: forohtooClient,
		metrics:        m,
		logger:         logger,
	}
}

// compile-time assertion that *helius.Client satisfies HeliusClientInterface.
var _ HeliusClientInterface = (*helius.Client)(nil)
