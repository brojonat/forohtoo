package solana

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// Well-known Solana program IDs
var (
	// SystemProgramID is the native SOL transfer program
	SystemProgramID = solana.MustPublicKeyFromBase58("11111111111111111111111111111112")

	// TokenProgramID is the SPL Token program
	TokenProgramID = solana.MustPublicKeyFromBase58("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")

	// Token2022ProgramID is the Token Extensions program (Token-2022)
	Token2022ProgramID = solana.MustPublicKeyFromBase58("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb")

	// MemoProgramIDSPL is the SPL Memo program (most common)
	MemoProgramIDSPL = solana.MustPublicKeyFromBase58("MemoSq4gqABAXKb96qnH8TysNcWxMyWCqXgDLGmfcHr")

	// MemoProgramIDLegacy is the legacy memo program (v1)
	MemoProgramIDLegacy = solana.MustPublicKeyFromBase58("Memo1UhkJRfHyvLMcVucJwxXeuD728EqVDDwQDxFMNo")
)

// System Program instruction types
const (
	SystemProgramTransferInstruction = uint32(2)
)

// Token Program instruction types
const (
	TokenProgramTransferInstruction        = uint8(3)
	TokenProgramTransferCheckedInstruction = uint8(12)
)

// signatureToDomain converts an RPC TransactionSignature to our domain Transaction.
// Note: This only includes metadata from the signature list, not full transaction details.
// For full details (amount, token mint, memo), call GetTransaction separately.
func signatureToDomain(sig *rpc.TransactionSignature) *Transaction {
	txn := &Transaction{
		Signature: sig.Signature.String(),
		Slot:      sig.Slot,
	}

	// Convert block time (Unix timestamp)
	if sig.BlockTime != nil {
		txn.BlockTime = sig.BlockTime.Time()
	} else {
		// If block time is nil, use zero time as fallback
		txn.BlockTime = time.Time{}
	}

	// Check if transaction failed
	if sig.Err != nil {
		errMsg := fmt.Sprintf("transaction failed: %v", sig.Err)
		txn.Err = &errMsg
	}

	// Note: Amount, TokenMint, and Memo are not available in the signature list.
	// These require fetching the full transaction via GetTransaction.

	return txn
}

// parseTransactionFromResult parses a full GetTransactionResult to extract transaction details.
// This extracts amount, token mint, and memo from the transaction instructions.
func parseTransactionFromResult(sig *rpc.TransactionSignature, result *rpc.GetTransactionResult) (*Transaction, error) {
	// Start with base transaction from signature metadata
	txn := signatureToDomain(sig)

	// If transaction failed, return early with just metadata
	if sig.Err != nil {
		return txn, nil
	}

	// Handle nil result (transaction not available)
	if result == nil {
		return txn, nil
	}

	// Decode the transaction
	tx, err := result.Transaction.GetTransaction()
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}

	// Parse instructions to extract transfer details and memo
	accountKeys := tx.Message.AccountKeys
	for _, instruction := range tx.Message.Instructions {
		programID := accountKeys[instruction.ProgramIDIndex]

		// Parse System Program transfers (native SOL)
		if programID.Equals(SystemProgramID) {
			if amount, fromAddr, err := parseSystemTransferWithSource(instruction, accountKeys); err == nil {
				txn.Amount = amount
				if fromAddr != nil {
					fromStr := fromAddr.String()
					txn.FromAddress = &fromStr
				}
				// token_mint stays nil for SOL
			}
		}

		// Parse SPL Token transfers (USDC, etc.)
		if programID.Equals(TokenProgramID) || programID.Equals(Token2022ProgramID) {
			if amount, mint, fromAddr, err := parseTokenTransferWithSource(instruction, accountKeys); err == nil {
				txn.Amount = amount
				if !mint.IsZero() {
					mintStr := mint.String()
					txn.TokenMint = &mintStr
				}
				if fromAddr != nil {
					fromStr := fromAddr.String()
					txn.FromAddress = &fromStr
				}
			}
		}

		// Parse memo
		if programID.Equals(MemoProgramIDSPL) || programID.Equals(MemoProgramIDLegacy) {
			if memo := parseMemo(instruction.Data); memo != "" {
				txn.Memo = &memo
			}
		}
	}

	return txn, nil
}

// parseSystemTransferWithSource extracts the amount and source address from a System Program Transfer instruction.
func parseSystemTransferWithSource(instruction solana.CompiledInstruction, accountKeys []solana.PublicKey) (uint64, *solana.PublicKey, error) {
	// System Transfer instruction format:
	// [0..4]  = instruction type (u32, should be 2 for Transfer)
	// [4..12] = lamports (u64)

	if len(instruction.Data) < 12 {
		return 0, nil, fmt.Errorf("instruction data too short: %d bytes", len(instruction.Data))
	}

	// Check instruction type
	instructionType := binary.LittleEndian.Uint32(instruction.Data[0:4])
	if instructionType != SystemProgramTransferInstruction {
		return 0, nil, fmt.Errorf("not a transfer instruction: type %d", instructionType)
	}

	// Extract amount
	amount := binary.LittleEndian.Uint64(instruction.Data[4:12])

	// Extract source address (first account in the instruction)
	// System Transfer accounts: [from, to]
	var fromAddr *solana.PublicKey
	if len(instruction.Accounts) >= 1 {
		fromAccountIndex := instruction.Accounts[0]
		if int(fromAccountIndex) < len(accountKeys) {
			addr := accountKeys[fromAccountIndex]
			fromAddr = &addr
		}
	}

	return amount, fromAddr, nil
}

// parseTokenTransferWithSource extracts amount, token mint, and source address from an SPL Token transfer instruction.
func parseTokenTransferWithSource(instruction solana.CompiledInstruction, accountKeys []solana.PublicKey) (amount uint64, mint solana.PublicKey, fromAddr *solana.PublicKey, err error) {
	if len(instruction.Data) == 0 {
		return 0, solana.PublicKey{}, nil, fmt.Errorf("empty instruction data")
	}

	instructionType := instruction.Data[0]

	switch instructionType {
	case TokenProgramTransferInstruction:
		// Transfer instruction format:
		// [0]     = instruction type (u8, 3 = Transfer)
		// [1..9]  = amount (u64)
		if len(instruction.Data) < 9 {
			return 0, solana.PublicKey{}, nil, fmt.Errorf("transfer instruction data too short")
		}
		amount = binary.LittleEndian.Uint64(instruction.Data[1:9])

		// Account layout for Transfer: [source, destination, authority]
		// Note: source is the token account, not the wallet owner
		// We'd need to look up the token account to find the owner
		// For now, return nil for from_address in this case
		return amount, solana.PublicKey{}, nil, nil

	case TokenProgramTransferCheckedInstruction:
		// TransferChecked instruction format:
		// [0]      = instruction type (u8, 12 = TransferChecked)
		// [1..9]   = amount (u64)
		// [9]      = decimals (u8)
		if len(instruction.Data) < 10 {
			return 0, solana.PublicKey{}, nil, fmt.Errorf("transferChecked instruction data too short")
		}
		amount = binary.LittleEndian.Uint64(instruction.Data[1:9])
		// decimals := instruction.Data[9] // Not needed for our purposes

		// For TransferChecked, the account layout is:
		// [source_token_account, mint, destination_token_account, authority, ...]
		if len(instruction.Accounts) < 4 {
			return 0, solana.PublicKey{}, nil, fmt.Errorf("transferChecked missing accounts")
		}

		// Get mint address
		mintAccountIndex := instruction.Accounts[1]
		if int(mintAccountIndex) >= len(accountKeys) {
			return 0, solana.PublicKey{}, nil, fmt.Errorf("mint account index out of bounds")
		}
		mint = accountKeys[mintAccountIndex]

		// Get authority (owner/signer) - this is the actual wallet that signed
		authorityIndex := instruction.Accounts[3]
		if int(authorityIndex) < len(accountKeys) {
			addr := accountKeys[authorityIndex]
			fromAddr = &addr
		}

		return amount, mint, fromAddr, nil

	default:
		return 0, solana.PublicKey{}, nil, fmt.Errorf("unknown token instruction type: %d", instructionType)
	}
}

// parseMemo extracts the memo text from a Memo Program instruction.
func parseMemo(data []byte) string {
	// Memo program instructions contain the memo as raw UTF-8 bytes
	// Some memos are base64 encoded, others are plain text
	// Try to decode as UTF-8 string first
	memo := string(data)

	// If it looks like base64, try decoding
	if decoded, err := base64.StdEncoding.DecodeString(memo); err == nil {
		// Check if decoded version is valid UTF-8
		if isValidUTF8(decoded) {
			return string(decoded)
		}
	}

	// Return as-is (plain UTF-8)
	return memo
}

// isValidUTF8 checks if bytes are valid UTF-8
func isValidUTF8(b []byte) bool {
	// Simple heuristic: check if there are any null bytes or invalid sequences
	for _, c := range b {
		if c == 0 {
			return false
		}
	}
	return true
}
