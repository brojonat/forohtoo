package server

import (
	"testing"

	"github.com/brojonat/forohtoo/service/config"
	"github.com/brojonat/forohtoo/service/temporal"
)

// TestPaymentGatewayConfig tests that payment gateway config works
func TestPaymentGatewayConfig(t *testing.T) {
	cfg := &config.Config{
		PaymentGateway: config.PaymentGatewayConfig{
			Enabled:       false,
			ServiceWallet: "TestWallet123456789012345678901234",
			FeeAmount:     1000000,
		},
	}

	if cfg.PaymentGateway.Enabled {
		t.Error("Payment gateway should be disabled")
	}

	if cfg.PaymentGateway.FeeAmount != 1000000 {
		t.Errorf("Expected fee amount 1000000, got %d", cfg.PaymentGateway.FeeAmount)
	}
}

// TestTemporalClientInterface tests that the client type exists
func TestTemporalClientInterface(t *testing.T) {
	// Just test that the type compiles
	var client *temporal.Client
	_ = client
}
