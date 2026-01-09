package solana

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectRandomEndpoint(t *testing.T) {
	t.Run("successful selection from multiple endpoints", func(t *testing.T) {
		endpoints := []string{
			"https://api.mainnet-beta.solana.com",
			"https://mainnet.helius-rpc.com",
			"https://rpc.ankr.com/solana",
		}

		selected, err := SelectRandomEndpoint(endpoints)
		require.NoError(t, err)
		assert.Contains(t, endpoints, selected)
	})

	t.Run("successful selection from single endpoint", func(t *testing.T) {
		endpoints := []string{"https://api.mainnet-beta.solana.com"}

		selected, err := SelectRandomEndpoint(endpoints)
		require.NoError(t, err)
		assert.Equal(t, endpoints[0], selected)
	})

	t.Run("error on empty slice", func(t *testing.T) {
		_, err := SelectRandomEndpoint([]string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no RPC endpoints configured")
	})

	t.Run("error on nil slice", func(t *testing.T) {
		_, err := SelectRandomEndpoint(nil)
		assert.Error(t, err)
	})

	t.Run("distribution across multiple calls", func(t *testing.T) {
		endpoints := []string{
			"https://endpoint1.com",
			"https://endpoint2.com",
			"https://endpoint3.com",
		}

		// Run 30 selections and verify we get different endpoints
		// (probabilistic test - very unlikely to select same endpoint 30 times)
		seen := make(map[string]bool)
		for i := 0; i < 30; i++ {
			selected, err := SelectRandomEndpoint(endpoints)
			require.NoError(t, err)
			seen[selected] = true
		}

		// With 30 selections from 3 endpoints, we should see at least 2 different ones
		assert.GreaterOrEqual(t, len(seen), 2, "Expected to see multiple endpoints selected")
	})
}
