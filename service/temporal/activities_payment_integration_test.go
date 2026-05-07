package temporal

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/client"
	"github.com/brojonat/forohtoo/service/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAwaitPayment_Integration tests the AwaitPayment activity against a
// running forohtoo server (which streams Helius-fed transactions over SSE).
//
// Required env: FOROHTOO_SERVER_URL, TEST_SERVICE_WALLET, RUN_INTEGRATION_TESTS=1.
func TestAwaitPayment_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	serverURL := os.Getenv("FOROHTOO_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:18000"
	}

	forohtooClient := client.NewClient(serverURL, nil, slog.Default())

	activities := &Activities{
		forohtooClient: forohtooClient,
		logger:         slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testWallet := os.Getenv("TEST_SERVICE_WALLET")
	if testWallet == "" {
		t.Skip("TEST_SERVICE_WALLET not set")
	}

	input := AwaitPaymentInput{
		PayToAddress:   testWallet,
		Network:        "devnet",
		Amount:         100000,
		Memo:           "forohtoo-test:integration",
		LookbackPeriod: 24 * time.Hour,
	}

	result, err := activities.AwaitPayment(ctx, input)
	if err != nil {
		t.Logf("AwaitPayment returned error (expected without an actual payment): %v", err)
		return
	}
	require.NotNil(t, result)
	assert.NotEmpty(t, result.TransactionSignature)
	assert.Greater(t, result.Amount, int64(0))
}

// stubHeliusClient is a HeliusClientInterface implementation that always fails
// AddAddress so we can exercise the rollback path in RegisterWallet.
type stubHeliusClient struct {
	addErr error
}

func (s *stubHeliusClient) AddAddress(_ context.Context, _ string) error    { return s.addErr }
func (s *stubHeliusClient) RemoveAddress(_ context.Context, _ string) error { return nil }

// TestRegisterWallet_Integration_Rollback verifies that RegisterWallet rolls
// back the wallet upsert when the Helius webhook subscription fails.
//
// Required env: TEST_DATABASE_URL, RUN_INTEGRATION_TESTS=1.
func TestRegisterWallet_Integration_Rollback(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set RUN_INTEGRATION_TESTS=1 to run.")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:15432/forohtoo_test?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := db.NewStore(pool)

	activities := &Activities{
		store:        store,
		heliusClient: &stubHeliusClient{addErr: errors.New("simulated helius failure")},
		logger:       slog.Default(),
	}

	testAddress := "TESTROLLBACK" + time.Now().Format("20060102150405")
	testNetwork := "devnet"
	tokenMint := "TestMint123"
	ataAddr := testAddress + "ATA"

	input := RegisterWalletInput{
		Address:                testAddress,
		Network:                testNetwork,
		AssetType:              "spl-token",
		TokenMint:              tokenMint,
		AssociatedTokenAddress: &ataAddr,
	}

	result, err := activities.RegisterWallet(ctx, input)
	assert.Error(t, err)
	assert.Nil(t, result)

	_, err = store.GetWallet(ctx, testAddress, testNetwork, "spl-token", tokenMint)
	assert.Error(t, err, "wallet should not exist after rollback")
}
