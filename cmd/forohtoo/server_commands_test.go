package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestHealthCommand_Success(t *testing.T) {
	// Create test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Set server URL
	os.Setenv("SERVER_URL", server.URL)
	defer os.Unsetenv("SERVER_URL")

	// Run command
	app := &cli.App{
		Name: "forohtoo",
		Commands: []*cli.Command{
			{
				Name: "server",
				Subcommands: []*cli.Command{
					healthCommand(),
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server-url",
				EnvVars: []string{"SERVER_URL"},
			},
		},
	}

	err := app.Run([]string{"forohtoo", "server", "health"})
	require.NoError(t, err)
}

func TestHealthCommand_Failure(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Set server URL
	os.Setenv("SERVER_URL", server.URL)
	defer os.Unsetenv("SERVER_URL")

	// Run command
	app := &cli.App{
		Name: "forohtoo",
		Commands: []*cli.Command{
			{
				Name: "server",
				Subcommands: []*cli.Command{
					healthCommand(),
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server-url",
				EnvVars: []string{"SERVER_URL"},
			},
		},
	}

	err := app.Run([]string{"forohtoo", "server", "health"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unhealthy status")
}

func TestHealthCommand_MissingServerURL(t *testing.T) {
	// Don't set SERVER_URL
	os.Unsetenv("SERVER_URL")

	app := &cli.App{
		Name: "forohtoo",
		Commands: []*cli.Command{
			{
				Name: "server",
				Subcommands: []*cli.Command{
					healthCommand(),
				},
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "server-url",
				EnvVars: []string{"SERVER_URL"},
			},
		},
	}

	err := app.Run([]string{"forohtoo", "server", "health"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server-url is required")
}

func TestVersionCommand(t *testing.T) {
	// Set version info
	version = "1.0.0"
	commit = "abc123"
	date = "2025-10-10"

	app := &cli.App{
		Name: "forohtoo",
		Commands: []*cli.Command{
			{
				Name: "server",
				Subcommands: []*cli.Command{
					versionCommand(),
				},
			},
		},
	}

	err := app.Run([]string{"forohtoo", "server", "version"})
	require.NoError(t, err)
}
