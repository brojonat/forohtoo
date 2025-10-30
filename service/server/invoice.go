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
type Invoice struct {
	ID           string        `json:"id"`             // Unique invoice ID (UUID)
	PayToAddress string        `json:"pay_to_address"` // Forohtoo's wallet
	Network      string        `json:"network"`        // "mainnet" or "devnet"
	Amount       int64         `json:"amount"`         // Lamports or token amount
	AmountSOL    float64       `json:"amount_sol"`     // Human-readable SOL amount
	AssetType    string        `json:"asset_type"`     // "sol" or "spl-token"
	TokenMint    string        `json:"token_mint,omitempty"` // For SPL tokens
	Memo         string        `json:"memo"`           // Required in payment txn
	ExpiresAt    time.Time     `json:"expires_at"`     // Payment deadline
	Timeout      time.Duration `json:"timeout"`        // Duration until expiry
	StatusURL    string        `json:"status_url"`     // Where to check payment status
	PaymentURL   string        `json:"payment_url"`    // Solana Pay URL for wallet apps
	QRCodeData   string        `json:"qr_code_data"`   // Base64 encoded QR code image
	CreatedAt    time.Time     `json:"created_at"`
}

// generatePaymentInvoice creates a new payment invoice for wallet registration.
func generatePaymentInvoice(cfg *config.PaymentGatewayConfig, address, network, assetType, tokenMint string) Invoice {
	invoiceID := uuid.New().String()
	memo := fmt.Sprintf("%s%s", cfg.MemoPrefix, invoiceID)
	now := time.Now()

	// Convert lamports to SOL for display
	amountSOL := float64(cfg.FeeAmount) / 1e9

	// Build Solana Pay URL
	paymentURL := buildSolanaPayURL(
		cfg.ServiceWallet,
		cfg.FeeAmount,
		cfg.FeeAssetType,
		cfg.FeeTokenMint,
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
		Amount:       cfg.FeeAmount,
		AmountSOL:    amountSOL,
		AssetType:    cfg.FeeAssetType,
		TokenMint:    cfg.FeeTokenMint,
		Memo:         memo,
		ExpiresAt:    now.Add(cfg.PaymentTimeout),
		Timeout:      cfg.PaymentTimeout,
		StatusURL:    fmt.Sprintf("/api/v1/registration-status/payment-registration:%s", invoiceID),
		PaymentURL:   paymentURL,
		QRCodeData:   qrCodeData,
		CreatedAt:    now,
	}
}

// buildSolanaPayURL creates a Solana Pay-compatible URL for payment.
// Format: solana:{recipient}?amount={amount}&spl-token={mint}&memo={memo}&label={label}&message={message}
func buildSolanaPayURL(recipient string, amountLamports int64, assetType, tokenMint, memo string) string {
	// Convert lamports to SOL
	amountSOL := float64(amountLamports) / 1e9

	params := url.Values{}
	params.Set("amount", fmt.Sprintf("%.9f", amountSOL))
	params.Set("memo", memo)
	params.Set("label", "Forohtoo Registration")
	params.Set("message", "Payment for wallet monitoring service")

	// Add spl-token parameter if paying with SPL token
	if assetType == "spl-token" && tokenMint != "" {
		params.Set("spl-token", tokenMint)
	}

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
