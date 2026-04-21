package helius

// Helius API types for webhook management and enhanced transaction payloads.
// Reference: https://docs.helius.dev/webhooks-and-websockets/webhooks

// CreateWebhookRequest is the request body for POST /v0/webhooks.
type CreateWebhookRequest struct {
	WebhookURL       string   `json:"webhookURL"`
	TransactionTypes []string `json:"transactionTypes"`
	AccountAddresses []string `json:"accountAddresses"`
	WebhookType      string   `json:"webhookType"`
	TxnStatus        string   `json:"txnStatus,omitempty"`
	AuthHeader       string   `json:"authHeader,omitempty"`
}

// UpdateWebhookRequest is the request body for PUT /v0/webhooks/{webhookID}.
type UpdateWebhookRequest struct {
	WebhookURL       string   `json:"webhookURL"`
	TransactionTypes []string `json:"transactionTypes"`
	AccountAddresses []string `json:"accountAddresses"`
	WebhookType      string   `json:"webhookType"`
	TxnStatus        string   `json:"txnStatus,omitempty"`
	AuthHeader       string   `json:"authHeader,omitempty"`
}

// Webhook is the response from the Helius webhooks API.
type Webhook struct {
	WebhookID        string   `json:"webhookID"`
	Wallet           string   `json:"wallet"`
	WebhookURL       string   `json:"webhookURL"`
	TransactionTypes []string `json:"transactionTypes"`
	AccountAddresses []string `json:"accountAddresses"`
	WebhookType      string   `json:"webhookType"`
	AuthHeader       string   `json:"authHeader,omitempty"`
}

// EnhancedTransaction is the payload Helius sends for "enhanced" webhook type.
// The webhook delivers an array of these.
type EnhancedTransaction struct {
	Signature        string             `json:"signature"`
	Slot             uint64             `json:"slot"`
	Timestamp        int64              `json:"timestamp"`
	Fee              uint64             `json:"fee"`
	FeePayer         string             `json:"feePayer"`
	Description      string             `json:"description"`
	Type             string             `json:"type"`
	Source           string             `json:"source"`
	NativeTransfers  []NativeTransfer   `json:"nativeTransfers"`
	TokenTransfers   []TokenTransfer    `json:"tokenTransfers"`
	AccountData      []AccountData      `json:"accountData"`
	TransactionError interface{}        `json:"transactionError"`
	Instructions     []InstructionGroup `json:"instructions"`
	Events           interface{}        `json:"events"`
}

// NativeTransfer represents a SOL transfer within a transaction.
type NativeTransfer struct {
	FromUserAccount string `json:"fromUserAccount"`
	ToUserAccount   string `json:"toUserAccount"`
	Amount          uint64 `json:"amount"`
}

// TokenTransfer represents an SPL token transfer within a transaction.
type TokenTransfer struct {
	FromTokenAccount string  `json:"fromTokenAccount"`
	FromUserAccount  string  `json:"fromUserAccount"`
	ToTokenAccount   string  `json:"toTokenAccount"`
	ToUserAccount    string  `json:"toUserAccount"`
	Mint             string  `json:"mint"`
	TokenAmount      float64 `json:"tokenAmount"`
	TokenStandard    string  `json:"tokenStandard"`
}

// AccountData represents account state changes in a transaction.
type AccountData struct {
	Account              string               `json:"account"`
	NativeBalanceChange  int64                `json:"nativeBalanceChange"`
	TokenBalanceChanges  []TokenBalanceChange `json:"tokenBalanceChanges"`
}

// TokenBalanceChange represents a token balance change for an account.
type TokenBalanceChange struct {
	UserAccount  string  `json:"userAccount"`
	TokenAccount string  `json:"tokenAccount"`
	Mint         string  `json:"mint"`
	RawAmount    string  `json:"rawTokenAmount"`
}

// InstructionGroup represents a parsed instruction in the transaction.
type InstructionGroup struct {
	Accounts        []string           `json:"accounts"`
	Data            string             `json:"data"`
	ProgramID       string             `json:"programId"`
	InnerInstructions []InstructionGroup `json:"innerInstructions"`
}
