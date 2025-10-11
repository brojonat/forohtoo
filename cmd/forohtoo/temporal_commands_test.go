package main

import (
	"bytes"
	"context"
	"os"
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
		ID:        "poll-wallet-" + address + "-{{.ScheduledTime.Unix}}",
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
