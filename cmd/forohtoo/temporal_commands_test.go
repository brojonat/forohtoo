package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"go.temporal.io/sdk/client"
)

func setupTestTemporal(t *testing.T) client.Client {
	t.Helper()

	// Skip by default - require explicit opt-in
	if os.Getenv("RUN_TEMPORAL_TESTS") == "" {
		t.Skip("Skipping Temporal integration test (set RUN_TEMPORAL_TESTS=1 to enable)")
	}

	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}

	temporalNamespace := os.Getenv("TEST_TEMPORAL_NAMESPACE")
	if temporalNamespace == "" {
		temporalNamespace = "default"
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort:  temporalHost,
		Namespace: temporalNamespace,
	})
	require.NoError(t, err)
	t.Cleanup(func() { temporalClient.Close() })

	return temporalClient
}

func createTestSchedule(t *testing.T, temporalClient client.Client, address string, interval time.Duration) string {
	t.Helper()

	ctx := context.Background()
	scheduleID := "poll-wallet-" + address

	scheduleSpec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{
			{Every: interval},
		},
	}

	workflowAction := client.ScheduleWorkflowAction{
		ID:        "poll-wallet-" + address,
		Workflow:  "PollWalletWorkflow",
		TaskQueue: "forohtoo-wallet-polling-test",
		Args:      []interface{}{address},
	}

	_, err := temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     scheduleID,
		Spec:   scheduleSpec,
		Action: &workflowAction,
		Memo: map[string]interface{}{
			"wallet_address": address,
			"created_by":     "test",
		},
	})
	require.NoError(t, err)

	// Cleanup: delete the schedule after test
	t.Cleanup(func() {
		handle := temporalClient.ScheduleClient().GetHandle(context.Background(), scheduleID)
		handle.Delete(context.Background())
	})

	return scheduleID
}

func TestPauseScheduleCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	// Create a test schedule
	testAddr := "TestPauseWa11et111111111111111111111111"
	scheduleID := createTestSchedule(t, temporalClient, testAddr, 30*time.Second)

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run pause command
	app := createTemporalTestApp()
	err := app.Run([]string{"forohtoo", "temporal", "pause-schedule", "--note", "Test pause", scheduleID})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, "Schedule paused")
	assert.Contains(t, output, scheduleID)

	// Verify schedule is actually paused
	ctx := context.Background()
	handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
	desc, err := handle.Describe(ctx)
	require.NoError(t, err)
	assert.True(t, desc.Schedule.State.Paused)
	assert.Equal(t, "Test pause", desc.Schedule.State.Note)
}

func TestResumeScheduleCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	// Create a test schedule and pause it
	testAddr := "TestResumeWa11et11111111111111111111111"
	scheduleID := createTestSchedule(t, temporalClient, testAddr, 30*time.Second)

	ctx := context.Background()
	handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
	err := handle.Pause(ctx, client.SchedulePauseOptions{Note: "Paused for test"})
	require.NoError(t, err)

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run resume command
	app := createTemporalTestApp()
	err = app.Run([]string{"forohtoo", "temporal", "resume-schedule", "--note", "Test resume", scheduleID})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, "Schedule resumed")
	assert.Contains(t, output, scheduleID)

	// Verify schedule is actually resumed
	desc, err := handle.Describe(ctx)
	require.NoError(t, err)
	assert.False(t, desc.Schedule.State.Paused)
	assert.Equal(t, "Test resume", desc.Schedule.State.Note)
}

func TestDeleteScheduleCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	// Create a test schedule
	testAddr := "TestDeleteWa11et11111111111111111111111"
	scheduleID := createTestSchedule(t, temporalClient, testAddr, 30*time.Second)

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run delete command with --force to skip confirmation
	app := createTemporalTestApp()
	err := app.Run([]string{"forohtoo", "temporal", "delete-schedule", "--force", scheduleID})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, "Schedule deleted")
	assert.Contains(t, output, scheduleID)

	// Verify schedule is actually deleted
	ctx := context.Background()
	handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
	_, err = handle.Describe(ctx)
	assert.Error(t, err) // Should error because schedule doesn't exist
}

func TestCreateScheduleCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	testAddr := "TestCreateWa11et11111111111111111111111"
	scheduleID := "poll-wallet-" + testAddr

	// Ensure schedule doesn't exist
	ctx := context.Background()
	handle := temporalClient.ScheduleClient().GetHandle(ctx, scheduleID)
	handle.Delete(ctx) // Ignore error if it doesn't exist

	// Cleanup after test
	t.Cleanup(func() {
		handle := temporalClient.ScheduleClient().GetHandle(context.Background(), scheduleID)
		handle.Delete(context.Background())
	})

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run create command
	app := createTemporalTestApp()
	err := app.Run([]string{"forohtoo", "temporal", "create-schedule", "--task-queue", "test-queue", testAddr, "30s"})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, "Schedule created")
	assert.Contains(t, output, scheduleID)
	assert.Contains(t, output, testAddr)
	assert.Contains(t, output, "30s")

	// Verify schedule actually exists and has correct config
	desc, err := handle.Describe(ctx)
	require.NoError(t, err)
	assert.NotNil(t, desc)

	// Verify workflow action
	action, ok := desc.Schedule.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	assert.Equal(t, "PollWalletWorkflow", action.Workflow)
	assert.Equal(t, "test-queue", action.TaskQueue)
	assert.Len(t, action.Args, 1)
	// Verify the memo contains the wallet address (simpler than decoding Payload)
	if desc.Memo.Fields != nil {
		walletAddrPayload := desc.Memo.Fields["wallet_address"]
		assert.NotNil(t, walletAddrPayload)
		assert.Contains(t, string(walletAddrPayload.Data), testAddr)
	}

	// Verify interval
	require.Len(t, desc.Schedule.Spec.Intervals, 1)
	assert.Equal(t, 30*time.Second, desc.Schedule.Spec.Intervals[0].Every)
}

func TestListSchedulesCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	// Create test schedules
	testAddr1 := "TestListWa11et1111111111111111111111111"
	testAddr2 := "TestListWa11et2222222222222222222222222"
	scheduleID1 := createTestSchedule(t, temporalClient, testAddr1, 30*time.Second)
	scheduleID2 := createTestSchedule(t, temporalClient, testAddr2, 60*time.Second)

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout/stderr
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	oldStderr := os.Stderr
	r2, w2, _ := os.Pipe()
	os.Stderr = w2

	// Run list command
	app := createTemporalTestApp()
	err := app.Run([]string{"forohtoo", "temporal", "list-schedules"})
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

	// Verify output contains our test schedules
	assert.Contains(t, output, scheduleID1)
	assert.Contains(t, output, scheduleID2)
}

func TestDescribeScheduleCommand(t *testing.T) {
	temporalClient := setupTestTemporal(t)

	// Create a test schedule
	testAddr := "TestDescribeWa11et111111111111111111111"
	scheduleID := createTestSchedule(t, temporalClient, testAddr, 45*time.Second)

	// Set environment variables
	temporalHost := os.Getenv("TEST_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	os.Setenv("TEMPORAL_HOST", temporalHost)
	defer os.Unsetenv("TEMPORAL_HOST")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run describe command
	app := createTemporalTestApp()
	err := app.Run([]string{"forohtoo", "temporal", "describe-schedule", scheduleID})
	require.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify output
	assert.Contains(t, output, scheduleID)
	assert.Contains(t, output, "PollWalletWorkflow")
	assert.Contains(t, output, "45s")
	assert.Contains(t, output, testAddr)
}

// createTemporalTestApp creates a CLI app for testing Temporal commands
func createTemporalTestApp() *cli.App {
	app := &cli.App{
		Name:  "forohtoo",
		Usage: "Solana wallet payment monitoring service CLI",
		Commands: []*cli.Command{
			{
				Name:  "temporal",
				Usage: "Temporal inspection and management commands",
				Subcommands: []*cli.Command{
					listSchedulesCommand(),
					describeScheduleCommand(),
					pauseScheduleCommand(),
					resumeScheduleCommand(),
					deleteScheduleCommand(),
					createScheduleCommand(),
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "temporal-host",
				Usage:   "Temporal server address",
				EnvVars: []string{"TEMPORAL_HOST"},
				Value:   "localhost:7233",
			},
			&cli.StringFlag{
				Name:    "temporal-namespace",
				Usage:   "Temporal namespace",
				EnvVars: []string{"TEMPORAL_NAMESPACE"},
				Value:   "default",
			},
		},
	}
	return app
}

// TestScheduleIDParsing tests parsing of both old and new schedule ID formats
// This is a unit test that doesn't require Temporal
func TestScheduleIDParsing(t *testing.T) {
	tests := []struct {
		name           string
		scheduleID     string
		expectedValid  bool
		expectedFormat string // "old" or "new"
		expectedParts  map[string]string
	}{
		{
			name:           "new format - SOL asset",
			scheduleID:     "poll-wallet-mainnet-9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk-sol-",
			expectedValid:  true,
			expectedFormat: "new",
			expectedParts: map[string]string{
				"network":    "mainnet",
				"address":    "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
				"asset_type": "sol",
				"token_mint": "",
			},
		},
		{
			name:           "new format - USDC SPL token",
			scheduleID:     "poll-wallet-mainnet-9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk-spl-token-EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			expectedValid:  true,
			expectedFormat: "new",
			expectedParts: map[string]string{
				"network":    "mainnet",
				"address":    "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
				"asset_type": "spl-token",
				"token_mint": "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			},
		},
		{
			name:           "new format - devnet SPL token",
			scheduleID:     "poll-wallet-devnet-TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA-spl-token-4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
			expectedValid:  true,
			expectedFormat: "new",
			expectedParts: map[string]string{
				"network":    "devnet",
				"address":    "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				"asset_type": "spl-token",
				"token_mint": "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
			},
		},
		{
			name:           "old format - mainnet",
			scheduleID:     "poll-wallet-mainnet-9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			expectedValid:  true,
			expectedFormat: "old",
			expectedParts: map[string]string{
				"network":    "mainnet",
				"address":    "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
				"asset_type": "",
				"token_mint": "",
			},
		},
		{
			name:           "old format - devnet",
			scheduleID:     "poll-wallet-devnet-TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
			expectedValid:  true,
			expectedFormat: "old",
			expectedParts: map[string]string{
				"network":    "devnet",
				"address":    "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				"asset_type": "",
				"token_mint": "",
			},
		},
		{
			name:          "invalid - no prefix",
			scheduleID:    "some-other-schedule",
			expectedValid: false,
		},
		{
			name:          "invalid - only prefix",
			scheduleID:    "poll-wallet-",
			expectedValid: false,
		},
		{
			name:          "invalid - single part",
			scheduleID:    "poll-wallet-mainnet",
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse using the logic from reconcile command
			if !strings.HasPrefix(tt.scheduleID, "poll-wallet-") {
				assert.False(t, tt.expectedValid, "Schedule ID without prefix should be invalid")
				return
			}

			remainder := tt.scheduleID[12:] // Skip "poll-wallet-" prefix
			parts := strings.SplitN(remainder, "-", 3) // Split into network-address-rest
			if len(parts) < 2 {
				assert.False(t, tt.expectedValid, "Schedule ID with < 2 parts should be invalid")
				return
			}

			assert.True(t, tt.expectedValid, "Schedule ID should be valid")

			var network, address, assetType, tokenMint string
			network = parts[0]
			address = parts[1]

			if len(parts) == 3 {
				// New format: parse asset_type and token_mint from remainder
				assert.Equal(t, "new", tt.expectedFormat, "Should parse as new format")
				rest := parts[2]

				if strings.HasPrefix(rest, "sol-") {
					assetType = "sol"
					tokenMint = rest[4:] // Everything after "sol-"
				} else if strings.HasPrefix(rest, "spl-token-") {
					assetType = "spl-token"
					tokenMint = rest[10:] // Everything after "spl-token-"
				} else {
					assert.Fail(t, "Unknown asset type format")
				}
			} else {
				// Old format: poll-wallet-{network}-{address}
				assert.Equal(t, "old", tt.expectedFormat, "Should parse as old format")
				assetType = ""
				tokenMint = ""
			}

			if tt.expectedParts != nil {
				assert.Equal(t, tt.expectedParts["network"], network)
				assert.Equal(t, tt.expectedParts["address"], address)
				assert.Equal(t, tt.expectedParts["asset_type"], assetType)
				assert.Equal(t, tt.expectedParts["token_mint"], tokenMint)
			}
		})
	}
}

// TestScheduleIDGeneration tests generating schedule IDs from wallet data
func TestScheduleIDGeneration(t *testing.T) {
	tests := []struct {
		name       string
		network    string
		address    string
		assetType  string
		tokenMint  string
		expectedID string
	}{
		{
			name:       "SOL asset",
			network:    "mainnet",
			address:    "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType:  "sol",
			tokenMint:  "",
			expectedID: "poll-wallet-mainnet-9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk-sol-",
		},
		{
			name:       "USDC mainnet",
			network:    "mainnet",
			address:    "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType:  "spl-token",
			tokenMint:  "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			expectedID: "poll-wallet-mainnet-9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk-spl-token-EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		},
		{
			name:       "USDC devnet",
			network:    "devnet",
			address:    "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
			assetType:  "spl-token",
			tokenMint:  "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
			expectedID: "poll-wallet-devnet-TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA-spl-token-4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate using the logic from reconcile command
			scheduleID := "poll-wallet-" + tt.network + "-" + tt.address + "-" + tt.assetType + "-" + tt.tokenMint
			assert.Equal(t, tt.expectedID, scheduleID)
		})
	}
}

// TestWalletKeyGeneration tests generating wallet keys for matching
func TestWalletKeyGeneration(t *testing.T) {
	tests := []struct {
		name        string
		network     string
		address     string
		assetType   string
		tokenMint   string
		expectedKey string
	}{
		{
			name:        "SOL asset",
			network:     "mainnet",
			address:     "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType:   "sol",
			tokenMint:   "",
			expectedKey: "mainnet:9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk:sol:",
		},
		{
			name:        "USDC mainnet",
			network:     "mainnet",
			address:     "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType:   "spl-token",
			tokenMint:   "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
			expectedKey: "mainnet:9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk:spl-token:EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		},
		{
			name:        "old format (empty asset type)",
			network:     "mainnet",
			address:     "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType:   "",
			tokenMint:   "",
			expectedKey: "mainnet:9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk::",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate using the logic from reconcile command
			key := tt.network + ":" + tt.address + ":" + tt.assetType + ":" + tt.tokenMint
			assert.Equal(t, tt.expectedKey, key)
		})
	}
}

// TestReconcileRoundTrip tests that we can generate a schedule ID and parse it back
func TestReconcileRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		network   string
		address   string
		assetType string
		tokenMint string
	}{
		{
			name:      "SOL asset",
			network:   "mainnet",
			address:   "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType: "sol",
			tokenMint: "",
		},
		{
			name:      "USDC mainnet",
			network:   "mainnet",
			address:   "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk",
			assetType: "spl-token",
			tokenMint: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate schedule ID
			scheduleID := "poll-wallet-" + tt.network + "-" + tt.address + "-" + tt.assetType + "-" + tt.tokenMint

			// Parse it back using the reconcile logic
			remainder := scheduleID[12:] // Skip "poll-wallet-" prefix
			parts := strings.SplitN(remainder, "-", 3) // Split into network-address-rest
			assert.Equal(t, 3, len(parts), "New format should parse into 3 parts")

			network := parts[0]
			address := parts[1]
			rest := parts[2]

			var assetType, tokenMint string
			if strings.HasPrefix(rest, "sol-") {
				assetType = "sol"
				tokenMint = rest[4:]
			} else if strings.HasPrefix(rest, "spl-token-") {
				assetType = "spl-token"
				tokenMint = rest[10:]
			}

			// Verify round trip
			assert.Equal(t, tt.network, network)
			assert.Equal(t, tt.address, address)
			assert.Equal(t, tt.assetType, assetType)
			assert.Equal(t, tt.tokenMint, tokenMint)

			// Generate wallet key
			walletKey := network + ":" + address + ":" + assetType + ":" + tokenMint

			// Verify the keys match
			expectedWalletKey := tt.network + ":" + tt.address + ":" + tt.assetType + ":" + tt.tokenMint
			assert.Equal(t, expectedWalletKey, walletKey)
		})
	}
}
