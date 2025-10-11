package solana

import (
	"time"

	"github.com/gagliardetto/solana-go"
)

// Transaction represents a parsed Solana transaction.
// This is our domain model, independent of the RPC response format.
type Transaction struct {
	Signature string
	Slot      uint64
	BlockTime time.Time
	Amount    uint64
	TokenMint *string // nil for native SOL transfers
	Memo      *string // parsed from transaction instructions
	Err       *string // nil if transaction succeeded, contains error message if failed
}

// GetTransactionsSinceParams contains parameters for polling new transactions.
type GetTransactionsSinceParams struct {
	Wallet        solana.PublicKey
	LastSignature *solana.Signature // nil = get most recent transactions
	Limit         int               // max transactions to return (default 100, max 1000)
}
