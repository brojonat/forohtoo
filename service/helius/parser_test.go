package helius

import (
	"log/slog"
	"os"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestParseEnhancedTransactions_NativeSOLTransfer(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"ReceiverWallet111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sig123abc",
			Slot:      100000,
			Timestamp: 1700000000,
			Fee:       5000,
			FeePayer:  "SenderWallet1111111111111111111111111111111",
			NativeTransfers: []NativeTransfer{
				{
					FromUserAccount: "SenderWallet1111111111111111111111111111111",
					ToUserAccount:   "ReceiverWallet111111111111111111111111111",
					Amount:          1_000_000_000, // 1 SOL
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())

	require.Len(t, results, 1)
	assert.Equal(t, "sig123abc", results[0].Signature)
	assert.Equal(t, "ReceiverWallet111111111111111111111111111", results[0].WalletAddress)
	assert.Equal(t, "mainnet", results[0].Network)
	assert.Equal(t, int64(100000), results[0].Slot)
	assert.Equal(t, int64(1_000_000_000), results[0].Amount)
	assert.Equal(t, "SenderWallet1111111111111111111111111111111", *results[0].FromAddress)
	assert.Nil(t, results[0].TokenMint)
	assert.Equal(t, "confirmed", results[0].ConfirmationStatus)
}

func TestParseEnhancedTransactions_SPLTokenTransfer(t *testing.T) {
	usdcMint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	addressMap := map[string]WalletLookup{
		"ReceiverATA1111111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "spl-token",
			TokenMint:     usdcMint,
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sig456def",
			Slot:      200000,
			Timestamp: 1700001000,
			TokenTransfers: []TokenTransfer{
				{
					FromUserAccount:  "SenderWallet1111111111111111111111111111111",
					FromTokenAccount: "SenderATA11111111111111111111111111111111",
					ToUserAccount:    "ReceiverWallet111111111111111111111111111",
					ToTokenAccount:   "ReceiverATA1111111111111111111111111111111",
					Mint:             usdcMint,
					TokenAmount:      5.0, // 5 USDC
					TokenStandard:    "Fungible",
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())

	require.Len(t, results, 1)
	assert.Equal(t, "sig456def", results[0].Signature)
	assert.Equal(t, "ReceiverWallet111111111111111111111111111", results[0].WalletAddress)
	assert.Equal(t, int64(5_000_000), results[0].Amount) // 5 USDC = 5_000_000 (6 decimals)
	assert.Equal(t, usdcMint, *results[0].TokenMint)
	assert.Equal(t, "SenderWallet1111111111111111111111111111111", *results[0].FromAddress)
}

func TestParseEnhancedTransactions_NoMatch(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"MonitoredWallet111111111111111111111111111": {
			WalletAddress: "MonitoredWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sigNoMatch",
			Slot:      300000,
			Timestamp: 1700002000,
			NativeTransfers: []NativeTransfer{
				{
					FromUserAccount: "SomeWallet111111111111111111111111111111",
					ToUserAccount:   "OtherWallet11111111111111111111111111111",
					Amount:          500_000_000,
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	assert.Empty(t, results)
}

func TestParseEnhancedTransactions_FailedTransaction(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"ReceiverWallet111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature:        "sigFailed",
			Slot:             400000,
			Timestamp:        1700003000,
			TransactionError: "InstructionError",
			NativeTransfers: []NativeTransfer{
				{
					FromUserAccount: "SenderWallet1111111111111111111111111111111",
					ToUserAccount:   "ReceiverWallet111111111111111111111111111",
					Amount:          1_000_000,
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	require.Len(t, results, 1)
	assert.Equal(t, "failed", results[0].ConfirmationStatus)
}

func TestParseEnhancedTransactions_WithMemo(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"ReceiverWallet111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "devnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sigMemo",
			Slot:      500000,
			Timestamp: 1700004000,
			NativeTransfers: []NativeTransfer{
				{
					FromUserAccount: "SenderWallet1111111111111111111111111111111",
					ToUserAccount:   "ReceiverWallet111111111111111111111111111",
					Amount:          1_000_000,
				},
			},
			Instructions: []InstructionGroup{
				{
					ProgramID: "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr",
					Data:      encodeMemo("hello world payment"),
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Memo)
	assert.Equal(t, "hello world payment", *results[0].Memo)
}

func TestParseEnhancedTransactions_MultipleTransfersInOneTx(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"Wallet1111111111111111111111111111111111111": {
			WalletAddress: "Wallet1111111111111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "sol",
		},
		"Wallet2222222222222222222222222222222222222": {
			WalletAddress: "Wallet2222222222222222222222222222222222222",
			Network:       "mainnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sigMulti",
			Slot:      600000,
			Timestamp: 1700005000,
			NativeTransfers: []NativeTransfer{
				{
					FromUserAccount: "SenderWallet1111111111111111111111111111111",
					ToUserAccount:   "Wallet1111111111111111111111111111111111111",
					Amount:          100_000_000,
				},
				{
					FromUserAccount: "SenderWallet1111111111111111111111111111111",
					ToUserAccount:   "Wallet2222222222222222222222222222222222222",
					Amount:          200_000_000,
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	require.Len(t, results, 2)
	assert.Equal(t, int64(100_000_000), results[0].Amount)
	assert.Equal(t, int64(200_000_000), results[1].Amount)
}

func TestParseEnhancedTransactions_MintMismatch(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"ReceiverATA1111111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "spl-token",
			TokenMint:     "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", // USDC
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sigWrongMint",
			Slot:      700000,
			Timestamp: 1700006000,
			TokenTransfers: []TokenTransfer{
				{
					ToTokenAccount: "ReceiverATA1111111111111111111111111111111",
					Mint:           "SomeOtherMint11111111111111111111111111111", // Wrong mint
					TokenAmount:    10.0,
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	assert.Empty(t, results, "should not match when mint doesn't match registered token")
}

func TestParseEnhancedTransactions_EmptyBatch(t *testing.T) {
	addressMap := map[string]WalletLookup{}
	results := ParseEnhancedTransactions(nil, addressMap, testLogger())
	assert.Empty(t, results)
}

func TestParseWebhookPayload(t *testing.T) {
	payload := `[{"signature":"sig1","slot":100,"timestamp":1700000000,"fee":5000,"feePayer":"abc","nativeTransfers":[],"tokenTransfers":[],"transactionError":null}]`
	txns, err := ParseWebhookPayload([]byte(payload))
	require.NoError(t, err)
	require.Len(t, txns, 1)
	assert.Equal(t, "sig1", txns[0].Signature)
	assert.Equal(t, uint64(100), txns[0].Slot)
}

func TestParseWebhookPayload_Invalid(t *testing.T) {
	_, err := ParseWebhookPayload([]byte("not json"))
	assert.Error(t, err)
}

func TestTokenAmountToRaw(t *testing.T) {
	// USDC: 6 decimals
	assert.Equal(t, int64(1_000_000), tokenAmountToRaw(1.0, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"))
	assert.Equal(t, int64(500_000), tokenAmountToRaw(0.5, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"))
	assert.Equal(t, int64(1_234_567), tokenAmountToRaw(1.234567, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"))

	// Unknown token defaults to 6 decimals
	assert.Equal(t, int64(1_000_000), tokenAmountToRaw(1.0, "unknown_mint"))
}

func TestExtractMemo_SPLMemoProgram(t *testing.T) {
	txn := EnhancedTransaction{
		Instructions: []InstructionGroup{
			{ProgramID: "11111111111111111111111111111112", Data: "transfer"},
			{ProgramID: "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr", Data: encodeMemo("my memo")},
		},
	}
	memo := extractMemo(txn)
	require.NotNil(t, memo)
	assert.Equal(t, "my memo", *memo)
}

func TestExtractMemo_LegacyMemoProgram(t *testing.T) {
	txn := EnhancedTransaction{
		Instructions: []InstructionGroup{
			{ProgramID: "Memo1UhkJRfHyvLMcVucJwxXeuD728EqVDDwQDxFMNo", Data: encodeMemo("legacy memo")},
		},
	}
	memo := extractMemo(txn)
	require.NotNil(t, memo)
	assert.Equal(t, "legacy memo", *memo)
}

func TestExtractMemo_InnerInstruction(t *testing.T) {
	txn := EnhancedTransaction{
		Instructions: []InstructionGroup{
			{
				ProgramID: "SomeProgram",
				InnerInstructions: []InstructionGroup{
					{ProgramID: "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr", Data: encodeMemo("inner memo")},
				},
			},
		},
	}
	memo := extractMemo(txn)
	require.NotNil(t, memo)
	assert.Equal(t, "inner memo", *memo)
}

func TestExtractMemo_NoMemo(t *testing.T) {
	txn := EnhancedTransaction{
		Instructions: []InstructionGroup{
			{ProgramID: "11111111111111111111111111111112", Data: "transfer"},
		},
	}
	assert.Nil(t, extractMemo(txn))
}

// Regression test for a real Phantom-paid Solana Pay transaction whose memo
// arrived from Helius as base58-encoded bytes; the prior implementation
// stored those bytes verbatim and downstream matchers couldn't find the
// expected workflow ID.
func TestExtractMemo_DecodesBase58FromHelius(t *testing.T) {
	want := "dms foobar@gmail.com Test-2026-05-07T23:56:11Z"
	txn := EnhancedTransaction{
		Instructions: []InstructionGroup{
			{ProgramID: "MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr", Data: encodeMemo(want)},
		},
	}
	memo := extractMemo(txn)
	require.NotNil(t, memo)
	assert.Equal(t, want, *memo)
}

// encodeMemo helper returns the base58 encoding of memo as it would appear in
// the Data field of a Helius enhanced transaction's memo program instruction.
func encodeMemo(memo string) string {
	return base58.Encode([]byte(memo))
}

func TestParseEnhancedTransactions_SOLAssetIgnoresTokenTransfer(t *testing.T) {
	addressMap := map[string]WalletLookup{
		"ReceiverWallet111111111111111111111111111": {
			WalletAddress: "ReceiverWallet111111111111111111111111111",
			Network:       "mainnet",
			AssetType:     "sol",
		},
	}

	txns := []EnhancedTransaction{
		{
			Signature: "sigTokenToSOLWallet",
			Slot:      800000,
			Timestamp: 1700007000,
			TokenTransfers: []TokenTransfer{
				{
					ToUserAccount:  "ReceiverWallet111111111111111111111111111",
					ToTokenAccount: "SomeATA111111111111111111111111111111111",
					Mint:           "SomeMint11111111111111111111111111111111",
					TokenAmount:    10.0,
				},
			},
		},
	}

	results := ParseEnhancedTransactions(txns, addressMap, testLogger())
	assert.Empty(t, results, "SOL-type wallet should not match token transfers via toUserAccount")
}
