package server

import (
	"encoding/base64"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/brojonat/forohtoo/service/config"
)

// TestGeneratePaymentInvoice tests basic invoice generation for USDC payments.
func TestGeneratePaymentInvoice(t *testing.T) {
	walletAddress := "TestWalletAddress123456789012345678901234"
	usdcMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mainnet
	cfg := &config.PaymentGatewayConfig{
		ServiceWallet:  "FoRoHtOoWaLLeTaDdReSs1234567890123456789012",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000, // 1 USDC
		PaymentTimeout: 24 * time.Hour,
		MemoPrefix:     "forohtoo-reg:",
	}

	beforeGeneration := time.Now()
	invoice := generatePaymentInvoice(cfg, walletAddress, usdcMint)
	afterGeneration := time.Now()

	// Verify invoice ID is the wallet address
	if invoice.ID != walletAddress {
		t.Errorf("Expected invoice ID to be wallet address %q, got %q", walletAddress, invoice.ID)
	}

	// Verify memo format
	expectedMemo := cfg.MemoPrefix + invoice.ID
	if invoice.Memo != expectedMemo {
		t.Errorf("Expected Memo %q, got %q", expectedMemo, invoice.Memo)
	}

	// Verify amount
	if invoice.Amount != cfg.FeeAmount {
		t.Errorf("Expected Amount %d, got %d", cfg.FeeAmount, invoice.Amount)
	}

	// Verify AmountUSDC calculation (base units to USDC)
	expectedAmountUSDC := float64(cfg.FeeAmount) / 1e6
	if invoice.AmountUSDC != expectedAmountUSDC {
		t.Errorf("Expected AmountUSDC %.6f, got %.6f", expectedAmountUSDC, invoice.AmountUSDC)
	}

	// Verify network
	if invoice.Network != cfg.ServiceNetwork {
		t.Errorf("Expected Network %q, got %q", cfg.ServiceNetwork, invoice.Network)
	}

	// Verify USDC mint
	if invoice.USDCMint != usdcMint {
		t.Errorf("Expected USDCMint %q, got %q", usdcMint, invoice.USDCMint)
	}

	// Verify pay to address
	if invoice.PayToAddress != cfg.ServiceWallet {
		t.Errorf("Expected PayToAddress %q, got %q", cfg.ServiceWallet, invoice.PayToAddress)
	}

	// Verify expiry
	if invoice.ExpiresAt.Before(beforeGeneration.Add(cfg.PaymentTimeout)) {
		t.Error("ExpiresAt should be at least timeout duration in the future")
	}
	if invoice.ExpiresAt.After(afterGeneration.Add(cfg.PaymentTimeout)) {
		t.Error("ExpiresAt should not be more than timeout duration after generation")
	}

	// Verify timeout duration
	if invoice.Timeout != cfg.PaymentTimeout {
		t.Errorf("Expected Timeout %v, got %v", cfg.PaymentTimeout, invoice.Timeout)
	}

	// Verify status URL format
	expectedStatusURL := "/api/v1/registration-status/payment-registration:" + invoice.ID
	if invoice.StatusURL != expectedStatusURL {
		t.Errorf("Expected StatusURL %q, got %q", expectedStatusURL, invoice.StatusURL)
	}

	// Verify payment URL is not empty
	if invoice.PaymentURL == "" {
		t.Error("PaymentURL should not be empty")
	}

	// Verify payment URL contains USDC mint (spl-token parameter)
	if !strings.Contains(invoice.PaymentURL, usdcMint) {
		t.Errorf("PaymentURL should contain USDC mint %q", usdcMint)
	}

	// Verify payment URL contains memo (URL-encoded)
	if !strings.Contains(invoice.PaymentURL, "memo=") {
		t.Error("PaymentURL should contain memo parameter")
	}

	// Verify QR code data is valid base64
	if invoice.QRCodeData == "" {
		t.Error("QRCodeData should not be empty")
	}
	_, err := base64.StdEncoding.DecodeString(invoice.QRCodeData)
	if err != nil {
		t.Errorf("QRCodeData should be valid base64: %v", err)
	}

	// Verify created at timestamp
	if invoice.CreatedAt.Before(beforeGeneration) || invoice.CreatedAt.After(afterGeneration) {
		t.Error("CreatedAt timestamp should be between test start and end")
	}
}

// TestBuildSolanaPayURL tests Solana Pay URL generation for USDC.
func TestBuildSolanaPayURL(t *testing.T) {
	recipient := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"
	amount := int64(1000000) // 1 USDC
	usdcMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	memo := "forohtoo-reg:test-invoice-123"

	paymentURL := buildSolanaPayURL(recipient, amount, usdcMint, memo)

	// Verify URL starts with solana: scheme
	if !strings.HasPrefix(paymentURL, "solana:") {
		t.Errorf("Payment URL should start with 'solana:', got %q", paymentURL)
	}

	// Verify URL contains recipient
	if !strings.Contains(paymentURL, recipient) {
		t.Errorf("Payment URL should contain recipient %q", recipient)
	}

	// Parse URL
	// Remove "solana:" prefix and parse
	urlWithoutScheme := strings.TrimPrefix(paymentURL, "solana:")
	parts := strings.SplitN(urlWithoutScheme, "?", 2)
	if len(parts) != 2 {
		t.Fatalf("Expected URL format solana:recipient?params, got %q", paymentURL)
	}

	params, err := url.ParseQuery(parts[1])
	if err != nil {
		t.Fatalf("Failed to parse URL params: %v", err)
	}

	// Verify amount parameter (1 USDC = 1.000000)
	expectedAmount := "1.000000"
	if params.Get("amount") != expectedAmount {
		t.Errorf("Expected amount=%q, got %q", expectedAmount, params.Get("amount"))
	}

	// Verify spl-token parameter (must be present for USDC)
	if params.Get("spl-token") != usdcMint {
		t.Errorf("Expected spl-token=%q, got %q", usdcMint, params.Get("spl-token"))
	}

	// Verify memo parameter
	if params.Get("memo") != memo {
		t.Errorf("Expected memo=%q, got %q", memo, params.Get("memo"))
	}

	// Verify label
	if params.Get("label") == "" {
		t.Error("Label parameter should not be empty")
	}

	// Verify message
	if params.Get("message") == "" {
		t.Error("Message parameter should not be empty")
	}
}

// TestGenerateQRCode tests QR code generation.
func TestGenerateQRCode(t *testing.T) {
	testURL := "solana:TestWallet?amount=1.0&memo=test"

	qrCodeData, err := generateQRCode(testURL)
	if err != nil {
		t.Fatalf("generateQRCode failed: %v", err)
	}

	// Verify result is not empty
	if qrCodeData == "" {
		t.Error("QR code data should not be empty")
	}

	// Verify result is valid base64
	decoded, err := base64.StdEncoding.DecodeString(qrCodeData)
	if err != nil {
		t.Errorf("QR code should be valid base64: %v", err)
	}

	// Verify decoded data is valid PNG
	_, err = png.Decode(strings.NewReader(string(decoded)))
	if err != nil {
		t.Errorf("QR code should be valid PNG image: %v", err)
	}
}

// TestGenerateQRCode_DifferentURLsProduceDifferentCodes tests that different URLs produce different QR codes.
func TestGenerateQRCode_DifferentURLsProduceDifferentCodes(t *testing.T) {
	url1 := "solana:Wallet1?amount=1.0"
	url2 := "solana:Wallet2?amount=2.0"

	qr1, err1 := generateQRCode(url1)
	qr2, err2 := generateQRCode(url2)

	if err1 != nil || err2 != nil {
		t.Fatalf("QR generation failed: err1=%v, err2=%v", err1, err2)
	}

	if qr1 == qr2 {
		t.Error("Different URLs should produce different QR codes")
	}
}
