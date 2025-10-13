package solana

import (
	"time"
)

// Transaction represents a parsed Solana transaction.
// This is our domain model, independent of the RPC response format.
type Transaction struct {
	Signature   string
	Slot        uint64
	BlockTime   time.Time
	Amount      uint64
	TokenMint   *string // nil for native SOL transfers
	Memo        *string // parsed from transaction instructions
	FromAddress *string // source wallet (sender), nil if cannot be determined
	Err         *string // nil if transaction succeeded, contains error message if failed
}
