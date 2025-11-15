package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func setupTestDB(t *testing.T) *db.Store {
	t.Helper()

	// Skip by default - require explicit opt-in
	if os.Getenv("RUN_DB_TESTS") == "" {
		t.Skip("Skipping database integration test (set RUN_DB_TESTS=1 to enable)")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	require.NoError(t, pool.Ping(context.Background()))

	// Clean database
	_, err = pool.Exec(context.Background(), "TRUNCATE TABLE transactions, wallets CASCADE")
	require.NoError(t, err)

	return db.NewStore(pool)
}

func TestListWalletsCommand(t *testing.T) {
	store := setupTestDB(t)

	// Create test wallets
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      "TestWa11et11111111111111111111111111111",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	_, err = store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      "TestWa11et22222222222222222222222222222",
		PollInterval: 30 * time.Second,
		Status:       "paused",
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		checkFunc      func(t *testing.T, output string)
	}{
		{
			name: "list all wallets",
			args: []string{"forohtoo", "db", "list-wallets"},
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "TestWa11et11111111111111111111111111111")
				assert.Contains(t, output, "TestWa11et22222222222222222222222222222")
				assert.Contains(t, output, "active")
				assert.Contains(t, output, "paused")
			},
		},
		{
			name: "filter by status",
			args: []string{"forohtoo", "db", "list-wallets", "--status", "active"},
			checkFunc: func(t *testing.T, output string) {
				assert.Contains(t, output, "TestWa11et11111111111111111111111111111")
				assert.NotContains(t, output, "TestWa11et22222222222222222222222222222")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set database URL for the command BEFORE creating app
			testDBURL := os.Getenv("TEST_DATABASE_URL")
			if testDBURL == "" {
				testDBURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
			}
			os.Setenv("DATABASE_URL", testDBURL)
			defer os.Unsetenv("DATABASE_URL")

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Capture stderr
			oldStderr := os.Stderr
			r2, w2, _ := os.Pipe()
			os.Stderr = w2

			// Run command
			app := createTestApp()
			err := app.Run(tt.args)
			require.NoError(t, err)

			// Restore stdout/stderr
			w.Close()
			w2.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read output
			var buf bytes.Buffer
			buf.ReadFrom(r)
			var buf2 bytes.Buffer
			buf2.ReadFrom(r2)

			output := buf.String() + buf2.String()
			if tt.checkFunc != nil {
				tt.checkFunc(t, output)
			}
		})
	}
}

func TestGetWalletCommand(t *testing.T) {
	store := setupTestDB(t)

	// Create test wallet
	wallet, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      "TestWa11et33333333333333333333333333333",
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Set database URL BEFORE creating app
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		testDBURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}
	os.Setenv("DATABASE_URL", testDBURL)
	defer os.Unsetenv("DATABASE_URL")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	app := createTestApp()
	err = app.Run([]string{"forohtoo", "db", "get-wallet", wallet.Address})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, wallet.Address)
	assert.Contains(t, output, "active")
}

func TestGetWalletCommand_NotFound(t *testing.T) {
	setupTestDB(t)

	// Set database URL BEFORE creating app
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		testDBURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}
	os.Setenv("DATABASE_URL", testDBURL)
	defer os.Unsetenv("DATABASE_URL")

	// Run command with non-existent wallet
	app := createTestApp()
	err := app.Run([]string{"forohtoo", "db", "get-wallet", "NonExistentWa11et111111111111111111111"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get wallet")
}

func TestListTransactionsCommand(t *testing.T) {
	store := setupTestDB(t)

	// Create test wallet
	walletAddr := "TestWa11et44444444444444444444444444444"
	_, err := store.CreateWallet(context.Background(), db.CreateWalletParams{
		Address:      walletAddr,
		PollInterval: 30 * time.Second,
		Status:       "active",
	})
	require.NoError(t, err)

	// Create test transactions
	now := time.Now()
	for i := 0; i < 3; i++ {
		_, err := store.CreateTransaction(context.Background(), db.CreateTransactionParams{
			Signature:          "sig" + string(rune('1'+i)) + "111111111111111111111111111111111111111111",
			WalletAddress:      walletAddr,
			Slot:               int64(1000 + i),
			BlockTime:          now.Add(time.Duration(i) * time.Minute),
			Amount:             int64(100 * (i + 1)),
			ConfirmationStatus: "confirmed",
		})
		require.NoError(t, err)
	}

	// Set database URL BEFORE creating app
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		testDBURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}
	os.Setenv("DATABASE_URL", testDBURL)
	defer os.Unsetenv("DATABASE_URL")

	// Capture stdout/stderr
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	oldStderr := os.Stderr
	r2, w2, _ := os.Pipe()
	os.Stderr = w2

	// Run command
	app := createTestApp()
	err = app.Run([]string{"forohtoo", "db", "list-transactions", "--wallet", walletAddr})
	require.NoError(t, err)

	// Restore stdout/stderr
	w.Close()
	w2.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	var buf2 bytes.Buffer
	buf2.ReadFrom(r2)

	stdout := buf.String()
	stderr := buf2.String()

	// Verify JSON output contains all transactions (default format is now JSON)
	assert.Contains(t, stdout, "sig1")
	assert.Contains(t, stdout, "sig2")
	assert.Contains(t, stdout, "sig3")
	assert.Contains(t, stdout, walletAddr)

	// Verify we got valid JSON with 3 transactions
	var transactions []db.Transaction
	err = json.Unmarshal([]byte(stdout), &transactions)
	require.NoError(t, err)
	assert.Len(t, transactions, 3)

	// Note: "Total:" message only appears in human format, not JSON
	_ = stderr // stderr not used in JSON mode
}

func TestListTransactionsCommand_RequiresWallet(t *testing.T) {
	setupTestDB(t)

	// Set database URL BEFORE creating app
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		testDBURL = "postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
	}
	os.Setenv("DATABASE_URL", testDBURL)
	defer os.Unsetenv("DATABASE_URL")

	// Run command without wallet flag
	app := createTestApp()
	err := app.Run([]string{"forohtoo", "db", "list-transactions"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "please specify --wallet")
}

// createTestApp creates a CLI app for testing
func createTestApp() *cli.App {
	app := &cli.App{
		Name:  "forohtoo",
		Usage: "Solana wallet payment monitoring service CLI",
		Commands: []*cli.Command{
			{
				Name:  "db",
				Usage: "Database inspection commands",
				Subcommands: []*cli.Command{
					listWalletsCommand(),
					getWalletCommand(),
					listTransactionsCommand(),
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "database-url",
				Usage:   "Database connection URL",
				EnvVars: []string{"DATABASE_URL"},
			},
			&cli.BoolFlag{
				Name:    "json",
				Aliases: []string{"j"},
				Usage:   "Output in JSON format",
			},
		},
	}
	return app
}
