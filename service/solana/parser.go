package solana

import (
	"fmt"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
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
	// For now, we leave them as zero values.
	// TODO: Optionally fetch full transaction details if needed.

	return txn
}

// parseTransaction parses a full GetTransactionResult to extract details.
// This is more complex and we'll implement it incrementally as needed.
func parseTransaction(result *rpc.GetTransactionResult) (*Transaction, error) {
	// TODO: Implement full transaction parsing
	// This will need to:
	// 1. Parse instruction data to find transfers
	// 2. Identify if it's SOL or SPL token
	// 3. Extract amount
	// 4. Find token mint (for SPL tokens)
	// 5. Parse memo instructions
	//
	// For now, return a basic transaction with just the metadata
	return nil, fmt.Errorf("full transaction parsing not yet implemented")
}
