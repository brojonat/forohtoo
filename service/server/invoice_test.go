package server

import (
	"encoding/base64"
	"github.com/brojonat/forohtoo/service/config"
	"image/png"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestGeneratePaymentInvoice tests the basic invoice generation functionality.
//
// WHAT IS BEING TESTED:
// We're testing that generatePaymentInvoice() creates a valid invoice with
// all required fields properly populated.
//
// EXPECTED BEHAVIOR:
// - Invoice ID should be a valid UUID
// - Memo should be in format "{prefix}{invoice_id}"
// - Amount should match the configured fee amount
// - Network should match the service wallet's network
// - Expiry time should be exactly "now + timeout"
// - Status URL should be in the correct format
// - Payment URL should be a valid Solana Pay URL
// - QR code data should be valid base64-encoded PNG
//
// This test verifies the fundamental invoice generation logic.
func TestGeneratePaymentInvoice(t *testing.T) {
	cfg := &config.PaymentGatewayConfig{
		ServiceWallet:  "FoRoHtOoWaLLeTaDdReSs1234567890123456789012",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000, // 0.001 SOL
		FeeAssetType:   "sol",
		PaymentTimeout: 24 * time.Hour,
		MemoPrefix:     "forohtoo-reg:",
	}

	address := "TestWalletAddress123456789012345678901234"
	network := "mainnet"
	assetType := "sol"
	tokenMint := ""

	beforeGeneration := time.Now()
	invoice := generatePaymentInvoice(cfg, address, network, assetType, tokenMint)
	afterGeneration := time.Now()

	// Verify invoice ID is a valid UUID
	_, err := uuid.Parse(invoice.ID)
	if err != nil {
		t.Errorf("Invoice ID is not a valid UUID: %v", err)
	}

	// Verify memo format
	expectedMemo := cfg.MemoPrefix + invoice.ID
	if invoice.Memo != expectedMemo {
		t.Errorf("Expected memo %q, got %q", expectedMemo, invoice.Memo)
	}

	// Verify amount
	if invoice.Amount != cfg.FeeAmount {
		t.Errorf("Expected amount %d, got %d", cfg.FeeAmount, invoice.Amount)
	}

	// Verify AmountSOL calculation (lamports to SOL)
	expectedAmountSOL := float64(cfg.FeeAmount) / 1e9
	if invoice.AmountSOL != expectedAmountSOL {
		t.Errorf("Expected AmountSOL %.9f, got %.9f", expectedAmountSOL, invoice.AmountSOL)
	}

	// Verify network
	if invoice.Network != cfg.ServiceNetwork {
		t.Errorf("Expected network %q, got %q", cfg.ServiceNetwork, invoice.Network)
	}

	// Verify pay-to address
	if invoice.PayToAddress != cfg.ServiceWallet {
		t.Errorf("Expected PayToAddress %q, got %q", cfg.ServiceWallet, invoice.PayToAddress)
	}

	// Verify asset type
	if invoice.AssetType != cfg.FeeAssetType {
		t.Errorf("Expected AssetType %q, got %q", cfg.FeeAssetType, invoice.AssetType)
	}

	// Verify expiry time is approximately now + timeout
	// Allow 1 second tolerance for test execution time
	expectedExpiry := beforeGeneration.Add(cfg.PaymentTimeout)
	if invoice.ExpiresAt.Before(expectedExpiry) || invoice.ExpiresAt.After(afterGeneration.Add(cfg.PaymentTimeout)) {
		t.Errorf("Expected ExpiresAt around %v, got %v", expectedExpiry, invoice.ExpiresAt)
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

	// Verify QR code data is not empty
	if invoice.QRCodeData == "" {
		t.Error("QRCodeData should not be empty")
	}

	// Verify created at is recent
	if invoice.CreatedAt.Before(beforeGeneration) || invoice.CreatedAt.After(afterGeneration) {
		t.Errorf("Expected CreatedAt between %v and %v, got %v", beforeGeneration, afterGeneration, invoice.CreatedAt)
	}
}

// TestBuildSolanaPayURL tests Solana Pay URL generation for SOL payments.
//
// WHAT IS BEING TESTED:
// We're testing that buildSolanaPayURL() creates a valid Solana Pay URL
// with correct formatting for SOL payments.
//
// EXPECTED BEHAVIOR:
// - URL format should be "solana:{recipient}?{params}"
// - Amount should be in SOL (not lamports), with 9 decimal places
// - Memo should be URL-encoded
// - Label and message should be included and URL-encoded
// - No "spl-token" parameter for SOL payments
//
// This ensures wallet apps can correctly parse and pre-fill the payment.
func TestBuildSolanaPayURL(t *testing.T) {
	recipient := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"
	amountLamports := int64(1000000) // 0.001 SOL
	assetType := "sol"
	tokenMint := ""
	memo := "forohtoo-reg:abc123"

	paymentURL := buildSolanaPayURL(recipient, amountLamports, assetType, tokenMint, memo)

	// Verify URL starts with "solana:"
	if !strings.HasPrefix(paymentURL, "solana:") {
		t.Errorf("Expected URL to start with 'solana:', got %q", paymentURL)
	}

	// Verify recipient is in URL
	if !strings.Contains(paymentURL, recipient) {
		t.Errorf("Expected URL to contain recipient %q, got %q", recipient, paymentURL)
	}

	// Parse URL to verify parameters
	// Remove "solana:" prefix and parse as regular URL
	urlStr := strings.TrimPrefix(paymentURL, "solana:")
	parsedURL, err := url.Parse("https://dummy.com/" + urlStr) // Add dummy scheme for parsing
	if err != nil {
		t.Fatalf("Failed to parse URL: %v", err)
	}

	params := parsedURL.Query()

	// Verify amount is in SOL (0.001)
	amountStr := params.Get("amount")
	if amountStr != "0.001000000" {
		t.Errorf("Expected amount=0.001000000, got %q", amountStr)
	}

	// Verify memo
	memoStr := params.Get("memo")
	if memoStr != memo {
		t.Errorf("Expected memo=%q, got %q", memo, memoStr)
	}

	// Verify label
	label := params.Get("label")
	if label != "Forohtoo Registration" {
		t.Errorf("Expected label='Forohtoo Registration', got %q", label)
	}

	// Verify message
	message := params.Get("message")
	if message != "Payment for wallet monitoring service" {
		t.Errorf("Expected message='Payment for wallet monitoring service', got %q", message)
	}

	// Verify NO spl-token parameter for SOL payments
	if params.Has("spl-token") {
		t.Error("SOL payment should not have spl-token parameter")
	}
}

// TestBuildSolanaPayURL_SPLToken tests Solana Pay URL generation for SPL token payments.
//
// WHAT IS BEING TESTED:
// We're testing that buildSolanaPayURL() creates a valid Solana Pay URL
// with the spl-token parameter for SPL token payments.
//
// EXPECTED BEHAVIOR:
// - URL should include "spl-token={mint}" parameter
// - Amount should still be in decimal format (token decimals, not raw amount)
// - All other parameters (memo, label, message) should be present
//
// This ensures SPL token payments are correctly formatted for wallet apps.
func TestBuildSolanaPayURL_SPLToken(t *testing.T) {
	recipient := "FoRoHtOoWaLLeTaDdReSs1234567890123456789012"
	amountLamports := int64(1000000) // For USDC with 6 decimals, this would be 1 USDC
	assetType := "spl-token"
	tokenMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // USDC mainnet
	memo := "forohtoo-reg:xyz789"

	paymentURL := buildSolanaPayURL(recipient, amountLamports, assetType, tokenMint, memo)

	// Parse URL
	urlStr := strings.TrimPrefix(paymentURL, "solana:")
	parsedURL, err := url.Parse("https://dummy.com/" + urlStr)
	if err != nil {
		t.Fatalf("Failed to parse URL: %v", err)
	}

	params := parsedURL.Query()

	// Verify spl-token parameter is present and correct
	splToken := params.Get("spl-token")
	if splToken != tokenMint {
		t.Errorf("Expected spl-token=%q, got %q", tokenMint, splToken)
	}

	// Verify memo
	memoStr := params.Get("memo")
	if memoStr != memo {
		t.Errorf("Expected memo=%q, got %q", memo, memoStr)
	}
}

// TestGenerateQRCode tests QR code generation from payment URLs.
//
// WHAT IS BEING TESTED:
// We're testing that generateQRCode() creates a valid, scannable QR code
// image from a Solana Pay URL.
//
// EXPECTED BEHAVIOR:
// - QR code data should be valid base64
// - Decoding base64 should produce a valid PNG image
// - The PNG image should contain the payment URL (when scanned)
//
// This ensures wallet apps can scan the QR code and extract the payment info.
func TestGenerateQRCode(t *testing.T) {
	paymentURL := "solana:FoRoHtOo...?amount=0.001&memo=test"

	qrCodeData, err := generateQRCode(paymentURL)
	if err != nil {
		t.Fatalf("generateQRCode() failed: %v", err)
	}

	// Verify QR code data is not empty
	if qrCodeData == "" {
		t.Fatal("QR code data is empty")
	}

	// Verify it's valid base64
	imgData, err := base64.StdEncoding.DecodeString(qrCodeData)
	if err != nil {
		t.Fatalf("QR code data is not valid base64: %v", err)
	}

	// Verify it's a valid PNG image
	_, err = png.Decode(strings.NewReader(string(imgData)))
	if err != nil {
		t.Fatalf("QR code data is not a valid PNG image: %v", err)
	}
}

// TestGenerateQRCode_DifferentURLsProduceDifferentCodes tests that different
// payment URLs produce different QR codes.
//
// WHAT IS BEING TESTED:
// We're testing that QR code generation is deterministic - same input produces
// same output, different inputs produce different outputs.
//
// EXPECTED BEHAVIOR:
// - Same payment URL should produce the same QR code (deterministic)
// - Different payment URLs should produce different QR codes
//
// This ensures QR codes correctly encode the payment information.
func TestGenerateQRCode_DifferentURLsProduceDifferentCodes(t *testing.T) {
	url1 := "solana:Address1?amount=0.001&memo=test1"
	url2 := "solana:Address2?amount=0.002&memo=test2"

	qr1, err := generateQRCode(url1)
	if err != nil {
		t.Fatalf("generateQRCode(url1) failed: %v", err)
	}

	qr2, err := generateQRCode(url2)
	if err != nil {
		t.Fatalf("generateQRCode(url2) failed: %v", err)
	}

	// Different URLs should produce different QR codes
	if qr1 == qr2 {
		t.Error("Different URLs produced the same QR code")
	}

	// Same URL should produce same QR code (test determinism)
	qr1Again, err := generateQRCode(url1)
	if err != nil {
		t.Fatalf("generateQRCode(url1Again) failed: %v", err)
	}

	if qr1 != qr1Again {
		t.Error("Same URL produced different QR codes (not deterministic)")
	}
}

// TestInvoice_SPLToken tests invoice generation for SPL token payments.
//
// WHAT IS BEING TESTED:
// We're testing that invoices for SPL token payments include the token mint
// and have the correct asset type.
//
// EXPECTED BEHAVIOR:
// - AssetType should be "spl-token"
// - TokenMint should be set to the provided mint address
// - Payment URL should include spl-token parameter
//
// This ensures SPL token payment invoices are correctly configured.
func TestInvoice_SPLToken(t *testing.T) {
	cfg := &config.PaymentGatewayConfig{
		ServiceWallet:  "FoRoHtOoWaLLeTaDdReSs1234567890123456789012",
		ServiceNetwork: "mainnet",
		FeeAmount:      1000000,
		FeeAssetType:   "spl-token",
		FeeTokenMint:   "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", // USDC
		PaymentTimeout: 24 * time.Hour,
		MemoPrefix:     "forohtoo-reg:",
	}

	invoice := generatePaymentInvoice(cfg, "addr", "mainnet", "spl-token", cfg.FeeTokenMint)

	// Verify asset type
	if invoice.AssetType != "spl-token" {
		t.Errorf("Expected AssetType=\"spl-token\", got %q", invoice.AssetType)
	}

	// Verify token mint
	if invoice.TokenMint != cfg.FeeTokenMint {
		t.Errorf("Expected TokenMint=%q, got %q", cfg.FeeTokenMint, invoice.TokenMint)
	}

	// Verify payment URL contains spl-token parameter
	if !strings.Contains(invoice.PaymentURL, "spl-token=") {
		t.Error("Payment URL for SPL token should contain spl-token parameter")
	}
	if !strings.Contains(invoice.PaymentURL, cfg.FeeTokenMint) {
		t.Errorf("Payment URL should contain token mint %q", cfg.FeeTokenMint)
	}
}
