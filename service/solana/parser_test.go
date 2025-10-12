package solana

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a TransactionResultEnvelope from a Transaction.
// Since TransactionResultEnvelope has unexported fields, we use JSON marshaling.
func makeTransactionEnvelope(tx *solana.Transaction) (*rpc.TransactionResultEnvelope, error) {
	// Marshal the transaction to JSON
	txJSON, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}

	// Create a temporary struct that matches the RPC response format
	var temp struct {
		Transaction json.RawMessage `json:"transaction"`
	}
	temp.Transaction = txJSON

	// Marshal and unmarshal to create the envelope
	envelopeJSON, err := json.Marshal(temp)
	if err != nil {
		return nil, err
	}

	var result rpc.GetTransactionResult
	if err := json.Unmarshal(envelopeJSON, &result); err != nil {
		return nil, err
	}

	return result.Transaction, nil
}

// TestParseTransaction_SOLTransfer tests parsing a native SOL transfer.
func TestParseTransaction_SOLTransfer(t *testing.T) {
	// Setup: Create a mock System Program transfer instruction
	fromAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112") // System Program ID
	toAddr := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")    // Wrapped SOL mint (valid addr)

	// Build System Transfer instruction data:
	// [0..4]  = instruction type (u32, 2 = Transfer)
	// [4..12] = lamports (u64)
	instructionData := make([]byte, 12)
	binary.LittleEndian.PutUint32(instructionData[0:4], SystemProgramTransferInstruction)
	binary.LittleEndian.PutUint64(instructionData[4:12], 1000000000) // 1 SOL in lamports

	// Create transaction with System Program instruction
	tx := &solana.Transaction{
		Message: solana.Message{
			AccountKeys: []solana.PublicKey{fromAddr, toAddr, SystemProgramID},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 2, // SystemProgramID
					Accounts:       []uint16{0, 1}, // from, to
					Data:           instructionData,
				},
			},
		},
	}

	// Create mock RPC result
	sig := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	sigData := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      100,
		BlockTime: &now,
		Err:       nil,
	}

	envelope, err := makeTransactionEnvelope(tx)
	require.NoError(t, err)

	result := &rpc.GetTransactionResult{
		Transaction: envelope,
	}

	// Act
	txn, err := parseTransactionFromResult(sigData, result)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig.String(), txn.Signature)
	assert.Equal(t, uint64(1000000000), txn.Amount)
	assert.Nil(t, txn.TokenMint) // SOL transfers have no token mint
	assert.NotNil(t, txn.FromAddress)
	assert.Equal(t, fromAddr.String(), *txn.FromAddress)
	assert.Nil(t, txn.Err)
}

// TestParseTransaction_SPLTokenTransfer tests parsing an SPL token transfer.
func TestParseTransaction_SPLTokenTransfer(t *testing.T) {
	// Setup: Create a mock Token Program TransferChecked instruction
	sourceTokenAccount := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	mintAddr := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v") // USDC mainnet
	destTokenAccount := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
	authority := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	// Build TransferChecked instruction data:
	// [0]      = instruction type (u8, 12 = TransferChecked)
	// [1..9]   = amount (u64)
	// [9]      = decimals (u8)
	instructionData := make([]byte, 10)
	instructionData[0] = TokenProgramTransferCheckedInstruction
	binary.LittleEndian.PutUint64(instructionData[1:9], 1000000) // 1 USDC (6 decimals)
	instructionData[9] = 6                                       // decimals

	// Create transaction with Token Program instruction
	tx := &solana.Transaction{
		Message: solana.Message{
			AccountKeys:  []solana.PublicKey{sourceTokenAccount, mintAddr, destTokenAccount, authority, TokenProgramID},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 4,                // TokenProgramID
					Accounts:       []uint16{0, 1, 2, 3}, // source, mint, dest, authority
					Data:           instructionData,
				},
			},
		},
	}

	// Create mock RPC result
	sig := solana.MustSignatureFromBase58("2TgM4N8qCMqLvfR8dxqTQgKygPNzT5KQkN5b5sT7eZPEkdxyLTXGnNQB3j7KG4DPFg5Qez5yNJBQRQ5r7DDnFfjG")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	sigData := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      100,
		BlockTime: &now,
		Err:       nil,
	}

	envelope, err := makeTransactionEnvelope(tx)
	require.NoError(t, err)

	result := &rpc.GetTransactionResult{
		Transaction: envelope,
	}

	// Act
	txn, err := parseTransactionFromResult(sigData, result)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig.String(), txn.Signature)
	assert.Equal(t, uint64(1000000), txn.Amount)
	assert.NotNil(t, txn.TokenMint)
	assert.Equal(t, mintAddr.String(), *txn.TokenMint)
	assert.NotNil(t, txn.FromAddress)
	assert.Equal(t, authority.String(), *txn.FromAddress)
	assert.Nil(t, txn.Err)
}

// TestParseTransaction_WithMemo tests parsing a transaction with a memo.
func TestParseTransaction_WithMemo(t *testing.T) {
	// Setup: Create a transaction with a memo instruction
	fromAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	toAddr := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

	// Build System Transfer instruction
	transferData := make([]byte, 12)
	binary.LittleEndian.PutUint32(transferData[0:4], SystemProgramTransferInstruction)
	binary.LittleEndian.PutUint64(transferData[4:12], 1000000000)

	// Build Memo instruction - memo data is just UTF-8 bytes
	memoText := `{"workflow_id": "test-123"}`
	memoData := []byte(memoText)

	// Create transaction with both instructions
	tx := &solana.Transaction{
		Message: solana.Message{
			AccountKeys: []solana.PublicKey{fromAddr, toAddr, SystemProgramID, MemoProgramIDSPL},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 2, // SystemProgramID
					Accounts:       []uint16{0, 1},
					Data:           transferData,
				},
				{
					ProgramIDIndex: 3, // MemoProgramIDSPL
					Accounts:       []uint16{},
					Data:           memoData,
				},
			},
		},
	}

	// Create mock RPC result
	sig := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	sigData := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      100,
		BlockTime: &now,
		Err:       nil,
	}

	envelope, err := makeTransactionEnvelope(tx)
	require.NoError(t, err)

	result := &rpc.GetTransactionResult{
		Transaction: envelope,
	}

	// Act
	txn, err := parseTransactionFromResult(sigData, result)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig.String(), txn.Signature)
	assert.Equal(t, uint64(1000000000), txn.Amount)
	assert.NotNil(t, txn.Memo)
	assert.Equal(t, memoText, *txn.Memo)
	assert.NotNil(t, txn.FromAddress)
	assert.Equal(t, fromAddr.String(), *txn.FromAddress)
}

// TestParseTransaction_NoMemo tests parsing a transaction without a memo.
func TestParseTransaction_NoMemo(t *testing.T) {
	// Setup: Create a simple SOL transfer without memo
	fromAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	toAddr := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

	instructionData := make([]byte, 12)
	binary.LittleEndian.PutUint32(instructionData[0:4], SystemProgramTransferInstruction)
	binary.LittleEndian.PutUint64(instructionData[4:12], 500000000)

	tx := &solana.Transaction{
		Message: solana.Message{
			AccountKeys: []solana.PublicKey{fromAddr, toAddr, SystemProgramID},
			Instructions: []solana.CompiledInstruction{
				{
					ProgramIDIndex: 2,
					Accounts:       []uint16{0, 1},
					Data:           instructionData,
				},
			},
		},
	}

	sig := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	sigData := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      100,
		BlockTime: &now,
		Err:       nil,
	}

	envelope, err := makeTransactionEnvelope(tx)
	require.NoError(t, err)

	result := &rpc.GetTransactionResult{
		Transaction: envelope,
	}

	// Act
	txn, err := parseTransactionFromResult(sigData, result)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig.String(), txn.Signature)
	assert.Equal(t, uint64(500000000), txn.Amount)
	assert.Nil(t, txn.Memo) // No memo instruction
	assert.NotNil(t, txn.FromAddress)
}

// TestParseTransaction_Failed tests parsing a failed transaction.
func TestParseTransaction_Failed(t *testing.T) {
	// Setup: Create a transaction that failed
	sig := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	sigData := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      100,
		BlockTime: &now,
		Err:       map[string]interface{}{"InstructionError": []interface{}{0, "InsufficientFunds"}},
	}

	// For failed transactions, we don't need to parse the full transaction
	result := &rpc.GetTransactionResult{}

	// Act
	txn, err := parseTransactionFromResult(sigData, result)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, sig.String(), txn.Signature)
	assert.NotNil(t, txn.Err)
	assert.Contains(t, *txn.Err, "transaction failed")
}

// TestConvertSignatureToDomain tests basic conversion from RPC signature to domain Transaction.
func TestConvertSignatureToDomain(t *testing.T) {
	// Setup
	sig := solana.MustSignatureFromBase58("5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7")
	now := solana.UnixTimeSeconds(time.Now().Unix())

	rpcSig := &rpc.TransactionSignature{
		Signature: sig,
		Slot:      12345,
		BlockTime: &now,
		Err:       nil,
	}

	// Act
	txn := signatureToDomain(rpcSig)

	// Assert
	assert.Equal(t, sig.String(), txn.Signature)
	assert.Equal(t, uint64(12345), txn.Slot)
	assert.Equal(t, now.Time(), txn.BlockTime)
	assert.Nil(t, txn.Err)
}

// TestParseMemo_PlainText tests parsing plain text memos.
func TestParseMemo_PlainText(t *testing.T) {
	memoText := "test payment"
	result := parseMemo([]byte(memoText))
	assert.Equal(t, memoText, result)
}

// TestParseMemo_Base64 tests parsing base64-encoded memos.
func TestParseMemo_Base64(t *testing.T) {
	originalText := "secret message"
	encoded := base64.StdEncoding.EncodeToString([]byte(originalText))
	result := parseMemo([]byte(encoded))
	assert.Equal(t, originalText, result)
}

// TestParseSystemTransfer tests parsing System Program transfer instructions.
func TestParseSystemTransfer(t *testing.T) {
	fromAddr := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	toAddr := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")

	instructionData := make([]byte, 12)
	binary.LittleEndian.PutUint32(instructionData[0:4], SystemProgramTransferInstruction)
	binary.LittleEndian.PutUint64(instructionData[4:12], 2000000000)

	instruction := solana.CompiledInstruction{
		ProgramIDIndex: 0,
		Accounts:       []uint16{0, 1},
		Data:           instructionData,
	}

	accountKeys := []solana.PublicKey{fromAddr, toAddr}

	// Act
	amount, from, err := parseSystemTransferWithSource(instruction, accountKeys)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, uint64(2000000000), amount)
	require.NotNil(t, from)
	assert.Equal(t, fromAddr.String(), from.String())
}

// TestParseTokenTransfer tests parsing Token Program transfer instructions.
func TestParseTokenTransfer(t *testing.T) {
	sourceTokenAccount := solana.MustPublicKeyFromBase58("11111111111111111111111111111112")
	mintAddr := solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
	destTokenAccount := solana.MustPublicKeyFromBase58("So11111111111111111111111111111111111111112")
	authority := solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	instructionData := make([]byte, 10)
	instructionData[0] = TokenProgramTransferCheckedInstruction
	binary.LittleEndian.PutUint64(instructionData[1:9], 5000000)
	instructionData[9] = 6

	instruction := solana.CompiledInstruction{
		ProgramIDIndex: 0,
		Accounts:       []uint16{0, 1, 2, 3},
		Data:           instructionData,
	}

	accountKeys := []solana.PublicKey{sourceTokenAccount, mintAddr, destTokenAccount, authority}

	// Act
	amount, mint, from, err := parseTokenTransferWithSource(instruction, accountKeys)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, uint64(5000000), amount)
	assert.Equal(t, mintAddr.String(), mint.String())
	require.NotNil(t, from)
	assert.Equal(t, authority.String(), from.String())
}
