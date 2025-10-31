package server

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"

	"github.com/brojonat/forohtoo/service/config"
)

// Invoice represents a payment invoice for wallet registration.
// All payments are in USDC.
type Invoice struct {
	ID           string        `json:"id"`             // Unique invoice ID (UUID)
	PayToAddress string        `json:"pay_to_address"` // Forohtoo's wallet
	Network      string        `json:"network"`        // "mainnet" or "devnet"
	USDCMint     string        `json:"usdc_mint"`      // USDC token mint address for the network
	Amount       int64         `json:"amount"`         // Amount in USDC base units (6 decimals)
	AmountUSDC   float64       `json:"amount_usdc"`    // Human-readable USDC amount
	Memo         string        `json:"memo"`           // Required in payment txn
	ExpiresAt    time.Time     `json:"expires_at"`     // Payment deadline
	Timeout      time.Duration `json:"timeout"`        // Duration until expiry
	StatusURL    string        `json:"status_url"`     // Where to check payment status
	PaymentURL   string        `json:"payment_url"`    // Solana Pay URL for wallet apps
	QRCodeData   string        `json:"qr_code_data"`   // Base64 encoded QR code image
	CreatedAt    time.Time     `json:"created_at"`
}

// generatePaymentInvoice creates a new payment invoice for wallet registration.
// Payment is always in USDC for the specified network.
func generatePaymentInvoice(cfg *config.PaymentGatewayConfig, usdcMint string) Invoice {
	invoiceID := uuid.New().String()
	memo := fmt.Sprintf("%s%s", cfg.MemoPrefix, invoiceID)
	now := time.Now()

	// Convert USDC base units to human-readable amount (USDC has 6 decimals)
	amountUSDC := float64(cfg.FeeAmount) / 1e6

	// Build Solana Pay URL for USDC payment
	paymentURL := buildSolanaPayURL(
		cfg.ServiceWallet,
		cfg.FeeAmount,
		usdcMint,
		memo,
	)

	// Generate QR code
	qrCodeData, err := generateQRCode(paymentURL)
	if err != nil {
		// Log error but continue - QR code is optional
		qrCodeData = ""
	}

	return Invoice{
		ID:           invoiceID,
		PayToAddress: cfg.ServiceWallet,
		Network:      cfg.ServiceNetwork,
		USDCMint:     usdcMint,
		Amount:       cfg.FeeAmount,
		AmountUSDC:   amountUSDC,
		Memo:         memo,
		ExpiresAt:    now.Add(cfg.PaymentTimeout),
		Timeout:      cfg.PaymentTimeout,
		StatusURL:    fmt.Sprintf("/api/v1/registration-status/payment-registration:%s", invoiceID),
		PaymentURL:   paymentURL,
		QRCodeData:   qrCodeData,
		CreatedAt:    now,
	}
}

// buildSolanaPayURL creates a Solana Pay-compatible URL for USDC payment.
// Format: solana:{recipient}?amount={amount}&spl-token={usdcMint}&memo={memo}&label={label}&message={message}
func buildSolanaPayURL(recipient string, amountBaseUnits int64, usdcMint, memo string) string {
	// Convert USDC base units to human-readable amount (6 decimals)
	amountUSDC := float64(amountBaseUnits) / 1e6

	params := url.Values{}
	params.Set("amount", fmt.Sprintf("%.6f", amountUSDC))
	params.Set("spl-token", usdcMint) // Always USDC
	params.Set("memo", memo)
	params.Set("label", "Forohtoo Registration")
	params.Set("message", "Payment for wallet monitoring service")

	return fmt.Sprintf("solana:%s?%s", recipient, params.Encode())
}

// generateQRCode creates a QR code image from a payment URL and returns it as base64-encoded PNG.
func generateQRCode(data string) (string, error) {
	// Generate QR code with medium error correction
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("failed to create QR code: %w", err)
	}

	// Encode as PNG (256x256 pixels)
	png, err := qr.PNG(256)
	if err != nil {
		return "", fmt.Errorf("failed to encode QR code as PNG: %w", err)
	}

	// Return base64-encoded PNG for easy embedding in JSON/HTML
	return base64.StdEncoding.EncodeToString(png), nil
}
