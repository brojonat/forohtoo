package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/urfave/cli/v2"
)

func healthCommand() *cli.Command {
	return &cli.Command{
		Name:  "health",
		Usage: "Check server health",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "Request timeout",
				Value: 5 * time.Second,
			},
		},
		Action: func(c *cli.Context) error {
			serverURL := c.String("server-url")
			if serverURL == "" {
				return fmt.Errorf("server-url is required (set SERVER_URL env var or use --server-url)")
			}

			client := &http.Client{
				Timeout: c.Duration("timeout"),
			}

			healthURL := serverURL + "/health"
			resp, err := client.Get(healthURL)
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				fmt.Printf("✓ Server is healthy (status: %d)\n", resp.StatusCode)
				fmt.Printf("  URL: %s\n", serverURL)
				return nil
			}

			return fmt.Errorf("server returned unhealthy status: %d", resp.StatusCode)
		},
	}
}

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Show version information",
		Action: func(c *cli.Context) error {
			fmt.Printf("forohtoo CLI\n")
			fmt.Printf("  Version: %s\n", version)
			fmt.Printf("  Commit:  %s\n", commit)
			fmt.Printf("  Built:   %s\n", date)
			return nil
		},
	}
}
