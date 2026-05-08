package helius

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brojonat/forohtoo/service/db"
	"github.com/mr-tron/base58"
)

// WalletLookup maps poll addresses (wallet address for SOL, ATA for SPL tokens)
// to their wallet registration info.
type WalletLookup struct {
	WalletAddress string
	Network       string
	AssetType     string
	TokenMint     string
}

// ParseEnhancedTransactions converts a batch of Helius enhanced transactions into
// db.CreateTransactionParams, matched against registered wallets.
//
// addressMap maps monitored addresses (wallet for SOL, ATA for SPL tokens) to WalletLookup.
// This allows us to determine which registered wallet a transaction belongs to.
func ParseEnhancedTransactions(
	txns []EnhancedTransaction,
	addressMap map[string]WalletLookup,
	logger *slog.Logger,
) []db.CreateTransactionParams {
	var results []db.CreateTransactionParams

	for _, txn := range txns {
		parsed := parseOneTransaction(txn, addressMap, logger)
		results = append(results, parsed...)
	}

	return results
}

func parseOneTransaction(
	txn EnhancedTransaction,
	addressMap map[string]WalletLookup,
	logger *slog.Logger,
) []db.CreateTransactionParams {
	var results []db.CreateTransactionParams
	blockTime := time.Unix(txn.Timestamp, 0).UTC()

	// Determine confirmation status
	confirmationStatus := "confirmed"
	if txn.TransactionError != nil {
		confirmationStatus = "failed"
	}

	// Extract memo from description or instructions
	memo := extractMemo(txn)

	// Match native SOL transfers against monitored wallet addresses
	for _, nt := range txn.NativeTransfers {
		lookup, ok := addressMap[nt.ToUserAccount]
		if !ok {
			continue
		}
		// Only match SOL asset type
		if lookup.AssetType != "sol" {
			continue
		}

		from := nt.FromUserAccount
		params := db.CreateTransactionParams{
			Signature:          txn.Signature,
			WalletAddress:      lookup.WalletAddress,
			Network:            lookup.Network,
			Slot:               int64(txn.Slot),
			BlockTime:          blockTime,
			Amount:             int64(nt.Amount),
			ConfirmationStatus: confirmationStatus,
			FromAddress:        &from,
		}
		if memo != nil {
			params.Memo = memo
		}

		results = append(results, params)

		logger.Debug("matched native transfer",
			"signature", txn.Signature,
			"wallet", lookup.WalletAddress,
			"amount", nt.Amount,
			"from", nt.FromUserAccount,
		)
	}

	// Match SPL token transfers against monitored ATAs
	for _, tt := range txn.TokenTransfers {
		// Check toTokenAccount (the ATA) against our monitored addresses
		lookup, ok := addressMap[tt.ToTokenAccount]
		if !ok {
			// Also check toUserAccount in case the user monitors by wallet address
			lookup, ok = addressMap[tt.ToUserAccount]
			if !ok {
				continue
			}
		}
		// Only match spl-token asset type with matching mint
		if lookup.AssetType != "spl-token" {
			continue
		}
		if lookup.TokenMint != "" && lookup.TokenMint != tt.Mint {
			continue
		}

		// Convert float token amount to raw integer amount
		// Helius provides tokenAmount as a float (e.g., 1.5 USDC = 1.5)
		// We need the raw amount (e.g., 1500000 for USDC with 6 decimals)
		rawAmount := tokenAmountToRaw(tt.TokenAmount, tt.Mint)

		from := tt.FromUserAccount
		mint := tt.Mint
		params := db.CreateTransactionParams{
			Signature:          txn.Signature,
			WalletAddress:      lookup.WalletAddress,
			Network:            lookup.Network,
			Slot:               int64(txn.Slot),
			BlockTime:          blockTime,
			Amount:             rawAmount,
			TokenMint:          &mint,
			ConfirmationStatus: confirmationStatus,
			FromAddress:        &from,
		}
		if memo != nil {
			params.Memo = memo
		}

		results = append(results, params)

		logger.Debug("matched token transfer",
			"signature", txn.Signature,
			"wallet", lookup.WalletAddress,
			"mint", tt.Mint,
			"amount", tt.TokenAmount,
			"raw_amount", rawAmount,
			"from", tt.FromUserAccount,
		)
	}

	return results
}

// extractMemo looks for memo data in the Helius enhanced transaction.
// Helius includes memo program data in the instructions list. The instruction
// data is base58-encoded raw bytes; the memo program's payload is just the
// UTF-8 memo text, so we decode it before returning.
func extractMemo(txn EnhancedTransaction) *string {
	memoPrograms := map[string]bool{
		"MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr": true,
		"Memo1UhkJRfHyvLMcVucJwxXeuD728EqVDDwQDxFMNo":  true,
	}

	for _, ix := range txn.Instructions {
		if memoPrograms[ix.ProgramID] {
			if memo, ok := decodeMemoData(ix.Data); ok {
				return &memo
			}
		}
		for _, inner := range ix.InnerInstructions {
			if memoPrograms[inner.ProgramID] {
				if memo, ok := decodeMemoData(inner.Data); ok {
					return &memo
				}
			}
		}
	}

	return nil
}

// decodeMemoData base58-decodes a memo program instruction data string and
// returns its UTF-8 contents. Returns ok=false when the input is empty,
// undecodable, or doesn't decode to valid UTF-8 (in which case we'd rather
// drop the memo than store garbage that no client can match against).
func decodeMemoData(data string) (string, bool) {
	if data == "" {
		return "", false
	}
	raw, err := base58.Decode(data)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	return string(raw), true
}

// tokenAmountToRaw converts a float token amount to raw integer amount.
// Uses known decimals for common tokens, defaults to 6 decimals (USDC standard).
func tokenAmountToRaw(amount float64, mint string) int64 {
	decimals := getTokenDecimals(mint)
	return int64(math.Round(amount * math.Pow10(decimals)))
}

// getTokenDecimals returns the number of decimals for known token mints.
func getTokenDecimals(mint string) int {
	// Well-known token decimals
	switch {
	case strings.Contains(mint, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"): // USDC mainnet
		return 6
	case strings.Contains(mint, "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"): // USDC devnet
		return 6
	case strings.Contains(mint, "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"): // USDT
		return 6
	case strings.Contains(mint, "So11111111111111111111111111111111111111112"): // Wrapped SOL
		return 9
	default:
		return 6 // Default to 6 decimals (most SPL tokens)
	}
}

// ParseWebhookPayload parses the raw webhook body from Helius.
// Helius sends an array of enhanced transactions.
func ParseWebhookPayload(body []byte) ([]EnhancedTransaction, error) {
	var txns []EnhancedTransaction
	if err := json.Unmarshal(body, &txns); err != nil {
		return nil, fmt.Errorf("failed to parse webhook payload: %w", err)
	}
	return txns, nil
}
