# Payment Gateway Specification

## Overview

This document specifies a "dogfooding" payment gateway feature where the forohtoo service requires payment before registering new wallets. The service will use its own client library to monitor for incoming payments, demonstrating the value of the service while creating a sustainable business model.

Users can register wallets via:
- **REST API** (`POST /api/v1/wallet-assets`) - Returns JSON responses
- **Web UI** (`/ui/register-wallet`) - Uses htmx for interactive payment dialogs with QR codes

## High-Level Design

### Current Behavior

Currently, `POST /api/v1/wallet-assets` accepts wallet registration requests and immediately:
1. Validates the request
2. Upserts the wallet in the database (creates or updates)
3. Creates/updates a Temporal schedule for polling
4. Returns success

### Proposed Behavior

With payment gating enabled:

**First Registration (wallet not in DB):**
1. Validate the request
2. **Check if wallet exists in database**
3. If wallet does NOT exist:
   - Generate payment invoice (amount, deadline, memo)
   - Return HTTP 402 Payment Required with payment details
   - Client sends payment to forohtoo's wallet
   - Server blocks internally until payment received
   - Once paid, proceed with registration
4. If wallet exists, proceed as normal (no payment required)

**Subsequent Registrations (wallet in DB):**
- Proceed immediately with upsert (no payment required)
- Update poll interval, status, or other parameters as needed

### Payment Flow

```
Client                          Server                      Blockchain
  │                               │                             │
  │ POST /api/v1/wallet-assets    │                             │
  ├──────────────────────────────>│                             │
  │                               │ Check DB: wallet exists?    │
  │                               │ NO → Generate invoice       │
  │                               │                             │
  │ 402 Payment Required          │                             │
  │ {amount, address, memo, ...}  │                             │
  │<──────────────────────────────┤                             │
  │                               │                             │
  │ (User sends payment)          │                             │
  ├───────────────────────────────┼────────────────────────────>│
  │                               │                             │
  │                               │ client.Await() blocks...    │
  │                               │<────────────────────────────┤
  │                               │ Payment received!           │
  │                               │ Upsert wallet + schedule    │
  │                               │                             │
  │ 201 Created (wallet registered)│                            │
  │<──────────────────────────────┤                             │
```

## Architecture Considerations

### Using Client Package in Service

**Question:** Will using the `client` package in the `service` package cause issues?

**Answer:** No, it's safe. Here's why:

#### Dependency Analysis

1. **Client Package (`client/wallet.go`):**
   - Imports: Standard library only (`context`, `net/http`, `encoding/json`, etc.)
   - No imports from `github.com/brojonat/forohtoo/service/*`
   - Pure HTTP client with no coupling to server internals

2. **Server Package (`service/server/handlers.go`):**
   - Could import `github.com/brojonat/forohtoo/client`
   - This creates a dependency: `service/server` → `client`
   - No circular dependency because `client` doesn't import `service`

3. **Dependency Graph:**
   ```
   service/server ──imports──> client
         │                       │
         │                       └──> stdlib only
         │
         └──imports──> service/db
         └──imports──> service/temporal
   ```

#### Benefits of Using the Client

1. **Dogfooding**: We use our own API, proving it works
2. **Less Code**: No need to duplicate SSE parsing logic
3. **Consistency**: Same behavior as external clients
4. **Testing**: If it works for us, it works for users
5. **Self-Service**: Forohtoo monitors its own wallet like any client would

#### Potential Concerns

**Concern:** Circular HTTP calls (server calling itself)?

**Answer:** No issue. The registration handler will:
- Start a background goroutine to await payment
- Immediately return 402 to the client
- The goroutine connects to the SSE endpoint (which is already running)
- No recursive HTTP calls; the goroutine is just another SSE client

**Concern:** Performance overhead?

**Answer:** Minimal. The client makes one SSE connection per pending registration. This is the same overhead as any external client waiting for a payment. The SSE connection is efficient and designed for this use case.

**Concern:** Error handling complexity?

**Answer:** Manageable. The await goroutine handles:
- Context cancellation (if client disconnects)
- SSE connection errors (retry logic in client)
- Timeout (payment deadline exceeded)

### Implementation Approach

#### Option 1: Temporal Workflow (Recommended)

Use a Temporal workflow to handle payment monitoring and registration. Workflows survive restarts and provide built-in state management.

**Workflow: `PaymentGatedRegistrationWorkflow`**

```go
// service/temporal/workflow_payment.go

// PaymentGatedRegistrationWorkflow handles wallet registration with payment gating.
// This workflow:
// 1. Generates a payment invoice
// 2. Waits for payment via AwaitPayment activity (uses client.Await)
// 3. Registers the wallet and creates Temporal schedule
// 4. Returns registration confirmation
func PaymentGatedRegistrationWorkflow(ctx workflow.Context, input PaymentGatedRegistrationInput) (*PaymentGatedRegistrationResult, error) {
    logger := workflow.GetLogger(ctx)
    logger.Info("PaymentGatedRegistrationWorkflow started",
        "address", input.Address,
        "network", input.Network,
        "asset_type", input.AssetType,
    )

    result := &PaymentGatedRegistrationResult{
        Address:   input.Address,
        Network:   input.Network,
        AssetType: input.AssetType,
        TokenMint: input.TokenMint,
    }

    // Configure activity options
    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: input.PaymentTimeout, // Long timeout for payment wait
        HeartbeatTimeout:    30 * time.Second,     // Heartbeat every 30s while waiting
        RetryPolicy: &temporalsdk.RetryPolicy{
            InitialInterval:    time.Second,
            BackoffCoefficient: 2.0,
            MaximumInterval:    30 * time.Second,
            MaximumAttempts:    3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    // Step 1: Await payment
    awaitInput := AwaitPaymentInput{
        PayToAddress:   input.ServiceWallet,
        Network:        input.ServiceNetwork,
        Amount:         input.FeeAmount,
        Memo:           input.PaymentMemo,
        LookbackPeriod: 24 * time.Hour, // Check last 24h in case payment came before workflow started
    }

    var awaitResult *AwaitPaymentResult
    err := workflow.ExecuteActivity(ctx, a.AwaitPayment, awaitInput).Get(ctx, &awaitResult)
    if err != nil {
        logger.Error("payment await failed", "error", err)
        errMsg := fmt.Sprintf("payment await failed: %v", err)
        result.Error = &errMsg
        return result, fmt.Errorf("payment await failed: %w", err)
    }

    logger.Info("payment received",
        "txn_signature", awaitResult.TransactionSignature,
        "amount", awaitResult.Amount,
    )

    result.PaymentSignature = &awaitResult.TransactionSignature
    result.PaymentAmount = awaitResult.Amount

    // Step 2: Register wallet
    registerInput := RegisterWalletInput{
        Address:                input.Address,
        Network:                input.Network,
        AssetType:              input.AssetType,
        TokenMint:              input.TokenMint,
        AssociatedTokenAddress: input.AssociatedTokenAddress,
        PollInterval:           input.PollInterval,
    }

    var registerResult *RegisterWalletResult
    err = workflow.ExecuteActivity(ctx, a.RegisterWallet, registerInput).Get(ctx, &registerResult)
    if err != nil {
        logger.Error("wallet registration failed", "error", err)
        errMsg := fmt.Sprintf("wallet registration failed: %v", err)
        result.Error = &errMsg
        return result, fmt.Errorf("wallet registration failed: %w", err)
    }

    logger.Info("wallet registered successfully",
        "address", input.Address,
        "network", input.Network,
        "asset_type", input.AssetType,
    )

    result.RegisteredAt = workflow.Now(ctx)
    result.Status = "completed"

    return result, nil
}

// PaymentGatedRegistrationInput contains input for payment-gated registration.
type PaymentGatedRegistrationInput struct {
    // Wallet to register
    Address                string        `json:"address"`
    Network                string        `json:"network"`
    AssetType              string        `json:"asset_type"`
    TokenMint              string        `json:"token_mint"`
    AssociatedTokenAddress *string       `json:"associated_token_address"`
    PollInterval           time.Duration `json:"poll_interval"`

    // Payment details
    ServiceWallet  string        `json:"service_wallet"`  // Forohtoo's wallet
    ServiceNetwork string        `json:"service_network"` // Where to monitor payment
    FeeAmount      int64         `json:"fee_amount"`
    PaymentMemo    string        `json:"payment_memo"`
    PaymentTimeout time.Duration `json:"payment_timeout"`
}

// PaymentGatedRegistrationResult contains the result of payment-gated registration.
type PaymentGatedRegistrationResult struct {
    Address          string     `json:"address"`
    Network          string     `json:"network"`
    AssetType        string     `json:"asset_type"`
    TokenMint        string     `json:"token_mint"`
    PaymentSignature *string    `json:"payment_signature,omitempty"`
    PaymentAmount    int64      `json:"payment_amount"`
    RegisteredAt     time.Time  `json:"registered_at"`
    Status           string     `json:"status"` // "pending", "completed", "failed"
    Error            *string    `json:"error,omitempty"`
}
```

**Activities:**

```go
// service/temporal/activities_payment.go

// AwaitPayment activity waits for a payment transaction to arrive.
// Uses the client library's Await() method to block until payment received.
func (a *Activities) AwaitPayment(ctx context.Context, input AwaitPaymentInput) (*AwaitPaymentResult, error) {
    a.logger.InfoContext(ctx, "waiting for payment",
        "address", input.PayToAddress,
        "network", input.Network,
        "amount", input.Amount,
        "memo", input.Memo,
    )

    // Send heartbeats while waiting (every 30s)
    // This lets Temporal know the activity is still alive
    heartbeatCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    go func() {
        ticker := time.NewTicker(25 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-heartbeatCtx.Done():
                return
            case <-ticker.C:
                activity.RecordHeartbeat(ctx, "waiting for payment")
            }
        }
    }()

    // Use client library to await payment
    txn, err := a.paymentClient.Await(ctx, input.PayToAddress, input.Network, input.LookbackPeriod, func(t *client.Transaction) bool {
        // Match on memo and minimum amount
        meetsAmount := t.Amount >= input.Amount
        matchesMemo := t.Memo == input.Memo

        a.logger.DebugContext(ctx, "checking transaction",
            "signature", t.Signature,
            "amount", t.Amount,
            "required_amount", input.Amount,
            "meets_amount", meetsAmount,
            "memo", t.Memo,
            "required_memo", input.Memo,
            "matches_memo", matchesMemo,
        )

        return meetsAmount && matchesMemo
    })

    if err != nil {
        return nil, fmt.Errorf("payment await failed: %w", err)
    }

    a.logger.InfoContext(ctx, "payment received",
        "txn_signature", txn.Signature,
        "amount", txn.Amount,
        "from", txn.FromAddress,
    )

    return &AwaitPaymentResult{
        TransactionSignature: txn.Signature,
        Amount:               txn.Amount,
        FromAddress:          txn.FromAddress,
        BlockTime:            txn.BlockTime,
    }, nil
}

// RegisterWallet activity registers a wallet asset and creates its Temporal schedule.
func (a *Activities) RegisterWallet(ctx context.Context, input RegisterWalletInput) (*RegisterWalletResult, error) {
    a.logger.InfoContext(ctx, "registering wallet",
        "address", input.Address,
        "network", input.Network,
        "asset_type", input.AssetType,
    )

    // Upsert wallet in database
    wallet, err := a.store.UpsertWallet(ctx, db.UpsertWalletParams{
        Address:                input.Address,
        Network:                input.Network,
        AssetType:              input.AssetType,
        TokenMint:              input.TokenMint,
        AssociatedTokenAddress: input.AssociatedTokenAddress,
        PollInterval:           input.PollInterval,
        Status:                 "active",
    })
    if err != nil {
        return nil, fmt.Errorf("failed to upsert wallet: %w", err)
    }

    // Create Temporal schedule for polling
    err = a.scheduler.UpsertWalletAssetSchedule(ctx,
        input.Address,
        input.Network,
        input.AssetType,
        input.TokenMint,
        input.AssociatedTokenAddress,
        input.PollInterval,
    )
    if err != nil {
        // Rollback wallet creation
        a.store.DeleteWallet(ctx, input.Address, input.Network, input.AssetType, input.TokenMint)
        return nil, fmt.Errorf("failed to create schedule: %w", err)
    }

    a.logger.InfoContext(ctx, "wallet registered successfully",
        "address", input.Address,
        "network", input.Network,
    )

    return &RegisterWalletResult{
        Address:   wallet.Address,
        Network:   wallet.Network,
        AssetType: wallet.AssetType,
        TokenMint: wallet.TokenMint,
        Status:    wallet.Status,
    }, nil
}

// AwaitPaymentInput contains parameters for awaiting payment.
type AwaitPaymentInput struct {
    PayToAddress   string        `json:"pay_to_address"`
    Network        string        `json:"network"`
    Amount         int64         `json:"amount"`
    Memo           string        `json:"memo"`
    LookbackPeriod time.Duration `json:"lookback_period"`
}

// AwaitPaymentResult contains the result of awaiting payment.
type AwaitPaymentResult struct {
    TransactionSignature string     `json:"transaction_signature"`
    Amount               int64      `json:"amount"`
    FromAddress          *string    `json:"from_address,omitempty"`
    BlockTime            time.Time  `json:"block_time"`
}

// RegisterWalletInput contains parameters for registering a wallet.
type RegisterWalletInput struct {
    Address                string        `json:"address"`
    Network                string        `json:"network"`
    AssetType              string        `json:"asset_type"`
    TokenMint              string        `json:"token_mint"`
    AssociatedTokenAddress *string       `json:"associated_token_address"`
    PollInterval           time.Duration `json:"poll_interval"`
}

// RegisterWalletResult contains the result of registering a wallet.
type RegisterWalletResult struct {
    Address   string `json:"address"`
    Network   string `json:"network"`
    AssetType string `json:"asset_type"`
    TokenMint string `json:"token_mint"`
    Status    string `json:"status"`
}
```

**HTTP Handler:**

```go
// service/server/handlers.go

func handleRegisterWalletAsset(store *db.Store, scheduler temporal.Scheduler, temporalClient temporalclient.Client, cfg *config.Config, logger *slog.Logger) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ... validation (same as before) ...

        // Check if wallet exists
        exists, err := store.WalletExists(ctx, address, network, assetType, tokenMint)
        if err != nil {
            writeError(w, "internal error", 500)
            return
        }

        if exists {
            // Existing wallet - proceed with upsert (no payment)
            // ... existing upsert logic ...
            return
        }

        // First registration - require payment
        if !cfg.PaymentGateway.Enabled {
            // Payment gateway disabled - proceed with free registration
            // ... existing upsert logic ...
            return
        }

        // Generate invoice
        invoice := generatePaymentInvoice(cfg, address, network, assetType, tokenMint)

        // Start Temporal workflow for payment-gated registration
        workflowID := fmt.Sprintf("payment-registration:%s", invoice.ID)
        workflowInput := temporal.PaymentGatedRegistrationInput{
            Address:                address,
            Network:                network,
            AssetType:              assetType,
            TokenMint:              tokenMint,
            AssociatedTokenAddress: ata,
            PollInterval:           pollInterval,
            ServiceWallet:          cfg.PaymentGateway.ServiceWallet,
            ServiceNetwork:         cfg.PaymentGateway.ServiceNetwork,
            FeeAmount:              cfg.PaymentGateway.FeeAmount,
            PaymentMemo:            invoice.Memo,
            PaymentTimeout:         cfg.PaymentGateway.PaymentTimeout,
        }

        workflowOptions := temporalclient.StartWorkflowOptions{
            ID:                       workflowID,
            TaskQueue:                "forohtoo",
            WorkflowExecutionTimeout: cfg.PaymentGateway.PaymentTimeout + 5*time.Minute, // Grace period
        }

        _, err = temporalClient.ExecuteWorkflow(ctx, workflowOptions, temporal.PaymentGatedRegistrationWorkflow, workflowInput)
        if err != nil {
            logger.Error("failed to start payment workflow", "error", err)
            writeError(w, "failed to start payment workflow", http.StatusInternalServerError)
            return
        }

        logger.Info("payment workflow started", "workflow_id", workflowID, "invoice_id", invoice.ID)

        // Return 402 Payment Required with invoice and workflow ID
        response := map[string]interface{}{
            "status":      "payment_required",
            "invoice":     invoice,
            "workflow_id": workflowID,
            "status_url":  fmt.Sprintf("/api/v1/registration-status/%s", workflowID),
        }
        writeJSON(w, response, http.StatusPaymentRequired)
    })
}

// GET /api/v1/registration-status/{workflow_id}
// Check the status of a pending registration workflow
func handleGetRegistrationStatus(temporalClient temporalclient.Client, logger *slog.Logger) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        workflowID := r.PathValue("workflow_id")

        // Query workflow status
        workflowRun := temporalClient.GetWorkflow(r.Context(), workflowID, "")

        var result temporal.PaymentGatedRegistrationResult
        err := workflowRun.Get(r.Context(), &result)

        if err != nil {
            // Check if workflow is still running
            describeResp, descErr := temporalClient.DescribeWorkflowExecution(r.Context(), workflowID, "")
            if descErr != nil {
                writeError(w, "workflow not found", http.StatusNotFound)
                return
            }

            // Workflow still running
            writeJSON(w, map[string]interface{}{
                "workflow_id": workflowID,
                "status":      "pending",
                "state":       describeResp.WorkflowExecutionInfo.Status.String(),
            }, http.StatusOK)
            return
        }

        // Workflow completed
        writeJSON(w, map[string]interface{}{
            "workflow_id":        workflowID,
            "status":             result.Status,
            "address":            result.Address,
            "network":            result.Network,
            "payment_signature":  result.PaymentSignature,
            "registered_at":      result.RegisteredAt,
            "error":              result.Error,
        }, http.StatusOK)
    })
}
```

**Pros:**
- ✅ **Survives restarts**: Temporal manages workflow state automatically
- ✅ **No database table needed**: Workflow state is stored in Temporal
- ✅ **Built-in monitoring**: View workflows in Temporal UI
- ✅ **Retry logic**: Activities automatically retry on failure
- ✅ **Timeout handling**: Workflow-level timeout enforcement
- ✅ **Heartbeats**: Activity stays alive during long waits
- ✅ **Client polling**: Status endpoint queries workflow state
- ✅ **Idempotent**: Workflow ID prevents duplicate workflows

**Cons:**
- Requires Temporal infrastructure (already in use)
- Slightly more code than goroutine approach
- Need to configure activity timeouts carefully

**Verdict:** ✅ **Strongly Recommended.** Temporal workflows are purpose-built for this exact use case.

### Payment UX: QR Codes and Wallet Deep Links

To improve the user experience, generate Solana Pay-compatible URLs and QR codes that pre-fill payment details in wallet apps.

**Solana Pay URL Format:**

```
solana:{recipient}?amount={amount}&spl-token={mint}&memo={memo}&label={label}&message={message}
```

**Example Invoice with Payment URL:**

```go
type Invoice struct {
    ID           string        `json:"id"`
    PayToAddress string        `json:"pay_to_address"`
    Network      string        `json:"network"`
    Amount       int64         `json:"amount"`        // Lamports
    AmountSOL    float64       `json:"amount_sol"`    // Human-readable SOL
    AssetType    string        `json:"asset_type"`
    TokenMint    string        `json:"token_mint,omitempty"`
    Memo         string        `json:"memo"`
    ExpiresAt    time.Time     `json:"expires_at"`
    StatusURL    string        `json:"status_url"`
    PaymentURL   string        `json:"payment_url"`   // Solana Pay URL
    QRCodeData   string        `json:"qr_code_data"`  // Base64 encoded QR code image
    CreatedAt    time.Time     `json:"created_at"`
}

func generatePaymentInvoice(cfg *config.PaymentGatewayConfig, address, network, assetType, tokenMint string) Invoice {
    invoiceID := uuid.New().String()
    memo := fmt.Sprintf("%s%s", cfg.MemoPrefix, invoiceID)
    now := time.Now()

    // Convert lamports to SOL for display
    amountSOL := float64(cfg.FeeAmount) / 1e9

    // Build Solana Pay URL
    paymentURL := buildSolanaPayURL(cfg.ServiceWallet, cfg.FeeAmount, cfg.FeeAssetType, cfg.FeeTokenMint, memo)

    // Generate QR code
    qrCodeData, _ := generateQRCode(paymentURL)

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
        StatusURL:    fmt.Sprintf("/api/v1/registration-status/payment-registration:%s", invoiceID),
        PaymentURL:   paymentURL,
        QRCodeData:   qrCodeData,
        CreatedAt:    now,
    }
}

func buildSolanaPayURL(recipient string, amountLamports int64, assetType, tokenMint, memo string) string {
    // Convert lamports to SOL
    amountSOL := float64(amountLamports) / 1e9

    params := url.Values{}
    params.Set("amount", fmt.Sprintf("%.9f", amountSOL))
    params.Set("memo", memo)
    params.Set("label", "Forohtoo Registration")
    params.Set("message", "Payment for wallet monitoring service")

    if assetType == "spl-token" && tokenMint != "" {
        params.Set("spl-token", tokenMint)
    }

    return fmt.Sprintf("solana:%s?%s", recipient, params.Encode())
}

func generateQRCode(data string) (string, error) {
    // Use a QR code library like github.com/skip2/go-qrcode
    qr, err := qrcode.New(data, qrcode.Medium)
    if err != nil {
        return "", err
    }

    png, err := qr.PNG(256) // 256x256 px
    if err != nil {
        return "", err
    }

    // Return base64-encoded PNG for easy embedding in JSON/HTML
    return base64.StdEncoding.EncodeToString(png), nil
}
```

**Client Experience:**

```json
{
  "status": "payment_required",
  "invoice": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "pay_to_address": "FoRoHtOoWaLLeTaDdReSs1234567890123456789",
    "network": "mainnet",
    "amount": 1000000,
    "amount_sol": 0.001,
    "asset_type": "sol",
    "memo": "forohtoo-reg:a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "expires_at": "2025-10-30T12:00:00Z",
    "status_url": "/api/v1/registration-status/payment-registration:a1b2c3d4",
    "payment_url": "solana:FoRoHtOoWaLLeTaDdReSs1234567890123456789?amount=0.001&memo=forohtoo-reg:a1b2c3d4&label=Forohtoo+Registration",
    "qr_code_data": "iVBORw0KGgoAAAANSUhEUgAA..."
  },
  "workflow_id": "payment-registration:a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

Users can:
1. **Scan QR code** with mobile wallet (Phantom, Solflare, etc.)
2. **Click payment URL** to open desktop wallet
3. **Manually copy** address and memo if preferred
4. **Poll status URL** to know when payment is confirmed

### Database Schema Changes

**No new tables required!** Temporal workflows store all state internally. This simplifies the implementation significantly compared to the goroutine approach.

### Configuration

Add configuration for the payment gateway:

```go
// service/config/config.go
type Config struct {
    // ... existing fields ...

    // Payment gateway settings
    PaymentGateway PaymentGatewayConfig `env:",prefix=PAYMENT_GATEWAY_"`
}

type PaymentGatewayConfig struct {
    Enabled         bool          `env:"ENABLED" envDefault:"false"`
    ServiceWallet   string        `env:"SERVICE_WALLET"`           // Forohtoo's wallet address
    ServiceNetwork  string        `env:"SERVICE_NETWORK"`          // "mainnet" or "devnet"
    FeeAmount       int64         `env:"FEE_AMOUNT" envDefault:"1000000"` // Lamports (0.001 SOL)
    FeeAssetType    string        `env:"FEE_ASSET_TYPE" envDefault:"sol"`
    FeeTokenMint    string        `env:"FEE_TOKEN_MINT"`           // For SPL token fees
    PaymentTimeout  time.Duration `env:"PAYMENT_TIMEOUT" envDefault:"24h"`
    MemoPrefix      string        `env:"MEMO_PREFIX" envDefault:"forohtoo-reg:"`
}
```

Environment variables:

```bash
# .env.server.prod (example)
PAYMENT_GATEWAY_ENABLED=true
PAYMENT_GATEWAY_SERVICE_WALLET=YourMainnetWalletAddressHere
PAYMENT_GATEWAY_SERVICE_NETWORK=mainnet
PAYMENT_GATEWAY_FEE_AMOUNT=1000000  # 0.001 SOL
PAYMENT_GATEWAY_FEE_ASSET_TYPE=sol
PAYMENT_GATEWAY_PAYMENT_TIMEOUT=24h
PAYMENT_GATEWAY_MEMO_PREFIX=forohtoo-reg:
```

### Invoice Generation

```go
type Invoice struct {
    ID           string        `json:"id"`            // Unique invoice ID (UUID)
    PayToAddress string        `json:"pay_to_address"` // Forohtoo's wallet
    Network      string        `json:"network"`       // "mainnet" or "devnet"
    Amount       int64         `json:"amount"`        // Lamports or token amount
    AssetType    string        `json:"asset_type"`    // "sol" or "spl-token"
    TokenMint    string        `json:"token_mint,omitempty"` // For SPL tokens
    Memo         string        `json:"memo"`          // Required in payment txn
    ExpiresAt    time.Time     `json:"expires_at"`    // Payment deadline
    Timeout      time.Duration `json:"timeout"`       // Duration until expiry
    StatusURL    string        `json:"status_url"`    // Where to check payment status
    CreatedAt    time.Time     `json:"created_at"`
}

func generatePaymentInvoice(cfg *config.PaymentGatewayConfig, address, network, assetType, tokenMint string) Invoice {
    invoiceID := uuid.New().String()
    memo := fmt.Sprintf("%s%s", cfg.MemoPrefix, invoiceID)
    now := time.Now()

    return Invoice{
        ID:           invoiceID,
        PayToAddress: cfg.ServiceWallet,
        Network:      cfg.ServiceNetwork, // Forohtoo's wallet network
        Amount:       cfg.FeeAmount,
        AssetType:    cfg.FeeAssetType,
        TokenMint:    cfg.FeeTokenMint,
        Memo:         memo,
        ExpiresAt:    now.Add(cfg.PaymentTimeout),
        Timeout:      cfg.PaymentTimeout,
        StatusURL:    fmt.Sprintf("/api/v1/payment-status/%s", invoiceID),
        CreatedAt:    now,
    }
}
```

## Edge Cases and Concerns

### 1. Server Restart During Payment Monitoring

**Problem:** What happens if the server restarts while waiting for payment?

**Solution:** ✅ **Temporal handles this automatically!**

- Workflows persist in Temporal's database
- When worker restarts, Temporal resumes in-progress workflows
- Activities pick up where they left off (SSE reconnects via `client.Await`)
- No manual restoration needed

This is one of the key benefits of using Temporal workflows over goroutines.

### 2. Payment Received But Registration Fails

**Problem:** User pays, but wallet upsert or schedule creation fails.

**Solution:** Temporal's retry policy handles this automatically:

- `RegisterWallet` activity has retry policy (3 attempts with exponential backoff)
- If all retries fail, workflow fails with error
- Error is visible in Temporal UI with full context (payment signature, etc.)
- Client sees error when polling status endpoint
- Support can:
  - View failed workflow in Temporal UI
  - See payment signature (proof of payment)
  - Manually trigger new workflow or directly register wallet
  - Or retry the failed workflow

### 3. Duplicate Payments

**Problem:** User pays multiple times for the same registration.

**Solution:**
- Workflow ID is deterministic: `payment-registration:{invoice_id}`
- Temporal prevents duplicate workflows with same ID
- If client retries registration POST, they get the existing workflow ID
- Invoice memo is unique per workflow
- First matching payment completes the workflow
- Additional payments with same memo are ignored (workflow already completed)

### 4. Payment with Wrong Amount

**Problem:** User sends less than required amount.

**Solution:**
- Matcher checks `t.Amount >= invoice.Amount`
- Partial payments are ignored
- Overpayments are accepted (user's choice)

### 5. Payment to Wrong Network

**Problem:** User sends payment on devnet instead of mainnet (or vice versa).

**Solution:**
- Invoice clearly specifies network
- Forohtoo monitors the correct network for its service wallet
- Wrong-network payments won't be detected
- User must send payment on correct network

### 6. Forohtoo's Service Wallet Not Registered

**Problem:** Forohtoo can't monitor its own wallet if it's not registered.

**Solution:**
- On server startup, auto-register the service wallet if payment gateway is enabled
- Skip payment requirement for self-registration (check in handler)
- Service wallet registration is free and automatic

```go
func (s *Server) ensureServiceWalletRegistered(ctx context.Context) error {
    if !s.cfg.PaymentGateway.Enabled {
        return nil // Payment gateway disabled, nothing to do
    }

    serviceWallet := s.cfg.PaymentGateway.ServiceWallet
    serviceNetwork := s.cfg.PaymentGateway.ServiceNetwork
    assetType := s.cfg.PaymentGateway.FeeAssetType
    tokenMint := s.cfg.PaymentGateway.FeeTokenMint

    exists, err := s.store.WalletExists(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
    if err != nil {
        return fmt.Errorf("failed to check service wallet: %w", err)
    }

    if exists {
        s.logger.Info("service wallet already registered", "address", serviceWallet)
        return nil
    }

    // Register service wallet (free, no payment required)
    s.logger.Info("registering service wallet", "address", serviceWallet)

    var ata *string
    if assetType == "spl-token" {
        ataAddr, err := computeAssociatedTokenAddress(serviceWallet, tokenMint)
        if err != nil {
            return fmt.Errorf("failed to compute ATA for service wallet: %w", err)
        }
        ata = &ataAddr
    }

    wallet, err := s.store.UpsertWallet(ctx, db.UpsertWalletParams{
        Address:      serviceWallet,
        Network:      serviceNetwork,
        AssetType:    assetType,
        TokenMint:    tokenMint,
        PollInterval: 30 * time.Second, // Poll service wallet frequently
        Status:       "active",
    })
    if err != nil {
        return fmt.Errorf("failed to register service wallet: %w", err)
    }

    err = s.scheduler.UpsertWalletAssetSchedule(ctx, serviceWallet, serviceNetwork, assetType, tokenMint, ata, wallet.PollInterval)
    if err != nil {
        s.store.DeleteWallet(ctx, serviceWallet, serviceNetwork, assetType, tokenMint)
        return fmt.Errorf("failed to create schedule for service wallet: %w", err)
    }

    s.logger.Info("service wallet registered successfully", "address", serviceWallet)
    return nil
}
```

### 7. Client Retries Registration Request

**Problem:** Client sends same registration request multiple times.

**Solution:**
- First request for new wallet: Generates invoice, starts workflow, returns 402
- Retry with same parameters:
  - Check if wallet now exists (workflow may have completed)
  - If exists: proceed with upsert (no payment)
  - If not exists and payment gateway enabled: generate new invoice with new workflow ID
  - Each retry gets a new workflow (new invoice ID, new memo)
  - User should use status endpoint to check existing workflow rather than retrying POST

### 8. Timeout Cleanup

**Problem:** Do failed/timed-out workflows accumulate?

**Solution:**
- Temporal has configurable retention policies for workflow history
- Default: 7-30 days depending on configuration
- Failed workflows show in Temporal UI for debugging
- Can set workflow execution retention in Temporal namespace config
- No manual cleanup needed

### 9. Historical Payments (Lookback)

**Problem:** User pays before workflow starts (e.g., they pay immediately after getting invoice but before workflow activity begins).

**Solution:** ✅ Already handled in the workflow!

- `AwaitPayment` activity uses `client.Await()` with 24-hour lookback
- Activity checks last 24 hours of transactions when it starts
- If matching payment exists, workflow completes immediately
- No waiting required if payment already arrived

## Security Considerations

### 1. Invoice ID Security

- Use UUIDs for invoice IDs (unpredictable)
- Don't expose sequential IDs (prevents enumeration)
- Rate limit payment status checks

### 2. Memo Injection

- Sanitize memo prefix configuration (no special characters)
- Validate memo format in matcher (exact prefix match)

### 3. Service Wallet Compromise

- If service wallet private key is compromised, attacker can steal funds
- Use cold storage or multi-sig for service wallet (operational concern)
- Monitor service wallet balance, alert on unexpected decreases

### 4. Denial of Service

- Rate limit registration endpoint
- Limit concurrent workflows per IP/address (check Temporal)
- Set reasonable payment timeout in workflow config
- Temporal limits max concurrent workflow executions per namespace

### 5. Race Conditions

- Workflow ID prevents duplicate workflows (Temporal enforces uniqueness)
- Wallet upsert is idempotent (safe to call multiple times)
- Schedule upsert is idempotent (updates if exists)

## Testing Strategy

This section enumerates all tests we'll write to ensure correct behavior and edge case handling. We'll use Temporal's test framework for workflow/activity tests.

### Unit Tests

**File: `service/server/invoice_test.go`**

1. **`TestGeneratePaymentInvoice`**
   - Invoice ID is a valid UUID
   - Memo format is `{prefix}{invoice_id}`
   - Amount matches config
   - Network matches service wallet network
   - Expiry time is `now + timeout`
   - Status URL is correct format

2. **`TestGeneratePaymentInvoice_UniqueIDs`**
   - Generate 1000 invoices
   - Assert all IDs are unique

3. **`TestBuildSolanaPayURL`**
   - SOL payment URL format is correct
   - SPL token payment URL includes `spl-token` parameter
   - Amount is converted from lamports to SOL correctly
   - Memo, label, message are URL-encoded
   - Example: `solana:ABC...?amount=0.001&memo=forohtoo-reg:123&label=Forohtoo+Registration`

4. **`TestBuildSolanaPayURL_SPLToken`**
   - URL includes `spl-token={mint}` parameter
   - Amount is in token decimals (not lamports)

5. **`TestGenerateQRCode`**
   - QR code is valid base64-encoded PNG
   - Decodes to valid PNG image
   - Contains the payment URL

**File: `service/server/handlers_payment_test.go`**

6. **`TestHandleRegisterWalletAsset_PaymentGatewayDisabled`**
   - Config: `PaymentGateway.Enabled = false`
   - New wallet registration proceeds immediately (no payment)
   - Returns 201 Created
   - Wallet is in database
   - Temporal schedule created

7. **`TestHandleRegisterWalletAsset_WalletExists`**
   - Config: `PaymentGateway.Enabled = true`
   - Wallet already exists in database
   - Registration proceeds without payment (upsert)
   - Returns 200 OK

8. **`TestHandleRegisterWalletAsset_NewWallet_PaymentRequired`**
   - Config: `PaymentGateway.Enabled = true`
   - New wallet (not in database)
   - Returns 402 Payment Required
   - Response includes invoice with all fields
   - Response includes workflow_id
   - Response includes status_url
   - Temporal workflow started (verify via mock)

9. **`TestHandleRegisterWalletAsset_InvalidAddress`**
   - Invalid Solana address format
   - Returns 400 Bad Request
   - No workflow started

10. **`TestHandleRegisterWalletAsset_UnsupportedTokenMint`**
    - SPL token mint not in supported list
    - Returns 400 Bad Request
    - Error message lists supported mints

11. **`TestHandleGetRegistrationStatus_WorkflowNotFound`**
    - Invalid workflow ID
    - Returns 404 Not Found

12. **`TestHandleGetRegistrationStatus_WorkflowRunning`**
    - Workflow is in progress
    - Returns 200 OK with status="pending"
    - Includes workflow state

13. **`TestHandleGetRegistrationStatus_WorkflowCompleted`**
    - Workflow completed successfully
    - Returns 200 OK with status="completed"
    - Includes payment_signature, registered_at

14. **`TestHandleGetRegistrationStatus_WorkflowFailed`**
    - Workflow failed (timeout or error)
    - Returns 200 OK with status="failed"
    - Includes error message

**File: `service/config/config_test.go`**

15. **`TestPaymentGatewayConfig_Defaults`**
    - Default values are correct
    - Enabled defaults to false
    - FeeAmount defaults to 1000000 (0.001 SOL)
    - PaymentTimeout defaults to 24h

16. **`TestPaymentGatewayConfig_Validation`**
    - ServiceWallet is required when enabled
    - ServiceWallet must be valid Solana address
    - FeeAmount must be positive
    - PaymentTimeout must be positive

### Activity Tests

**File: `service/temporal/activities_payment_test.go`**

These tests use mocks for dependencies (client, store, scheduler).

17. **`TestAwaitPayment_Success`**
    - Mock client.Await returns matching transaction
    - Activity completes successfully
    - Returns transaction signature, amount, from_address
    - Heartbeats recorded during wait

18. **`TestAwaitPayment_TimeoutNoPayment`**
    - Mock client.Await times out (context deadline exceeded)
    - Activity returns error
    - Error is retryable (for Temporal retry policy)

19. **`TestAwaitPayment_AmountTooLow`**
    - Mock client.Await returns transaction with amount < required
    - Transaction is rejected by matcher
    - Activity continues waiting (or times out)

20. **`TestAwaitPayment_WrongMemo`**
    - Mock client.Await returns transaction with different memo
    - Transaction is rejected by matcher
    - Activity continues waiting

21. **`TestAwaitPayment_ExactAmount`**
    - Payment amount exactly equals required amount
    - Matcher accepts it
    - Activity succeeds

22. **`TestAwaitPayment_Overpayment`**
    - Payment amount > required amount
    - Matcher accepts it (>= check)
    - Activity succeeds

23. **`TestAwaitPayment_HistoricalPayment`**
    - Payment already exists in last 24h
    - Mock client.Await returns it immediately
    - Activity completes without waiting

24. **`TestAwaitPayment_ClientError`**
    - Mock client.Await returns error (network failure, etc.)
    - Activity returns error
    - Error is retryable

25. **`TestRegisterWallet_Success`**
    - Mock store.UpsertWallet succeeds
    - Mock scheduler.UpsertWalletAssetSchedule succeeds
    - Activity completes successfully
    - Returns wallet details

26. **`TestRegisterWallet_DatabaseError`**
    - Mock store.UpsertWallet returns error
    - Activity returns error
    - Error is retryable
    - Schedule is NOT created

27. **`TestRegisterWallet_ScheduleError`**
    - Mock store.UpsertWallet succeeds
    - Mock scheduler.UpsertWalletAssetSchedule returns error
    - Activity returns error
    - Rollback: wallet is deleted from database
    - Error is retryable

28. **`TestRegisterWallet_ScheduleErrorRollbackFails`**
    - Schedule creation fails
    - Wallet deletion also fails
    - Activity returns error with context about partial state

29. **`TestRegisterWallet_SPLToken`**
    - Asset type is "spl-token"
    - ATA is computed and passed to schedule
    - Both wallet and schedule use correct ATA

30. **`TestRegisterWallet_SOL`**
    - Asset type is "sol"
    - ATA is nil
    - Schedule created for SOL polling

### Workflow Tests

**File: `service/temporal/workflow_payment_test.go`**

These tests use Temporal's test framework (`testsuite.WorkflowTestSuite`).

31. **`TestPaymentGatedRegistrationWorkflow_Success`**
    - Mock AwaitPayment activity succeeds
    - Mock RegisterWallet activity succeeds
    - Workflow completes successfully
    - Result includes payment signature and registered_at
    - Status is "completed"

32. **`TestPaymentGatedRegistrationWorkflow_PaymentTimeout`**
    - Mock AwaitPayment activity times out
    - Workflow fails with timeout error
    - Result includes error message
    - Status is "failed"

33. **`TestPaymentGatedRegistrationWorkflow_RegistrationFails`**
    - Mock AwaitPayment succeeds
    - Mock RegisterWallet fails (all retries exhausted)
    - Workflow fails with error
    - Result includes payment signature (proof of payment)
    - Result includes error message about registration failure

34. **`TestPaymentGatedRegistrationWorkflow_RegistrationRetriesSucceed`**
    - Mock AwaitPayment succeeds
    - Mock RegisterWallet fails twice, succeeds on third attempt
    - Workflow completes successfully
    - Verifies retry policy is working

35. **`TestPaymentGatedRegistrationWorkflow_ActivityOptions`**
    - Verify StartToCloseTimeout is set to PaymentTimeout
    - Verify HeartbeatTimeout is 30s
    - Verify RetryPolicy has correct parameters

36. **`TestPaymentGatedRegistrationWorkflow_WorkflowTimeout`**
    - Set workflow execution timeout
    - Workflow times out before payment received
    - Workflow terminates with timeout error

### Integration Tests

**File: `service/server/integration_payment_test.go`**

These tests use real Temporal server (or Temporal test server) and real database (test container).

37. **`TestPaymentFlow_EndToEnd_Success`**
    - Start server with payment gateway enabled
    - Ensure service wallet is registered
    - POST /api/v1/wallet-assets for new wallet
    - Receive 402 Payment Required with invoice
    - Simulate payment (insert transaction into database)
    - Poll GET /api/v1/registration-status/{workflow_id}
    - Eventually returns status="completed"
    - Verify wallet in database
    - Verify Temporal schedule exists
    - Cleanup: delete wallet and schedule

38. **`TestPaymentFlow_EndToEnd_Timeout`**
    - POST /api/v1/wallet-assets
    - Receive 402 Payment Required
    - Do NOT send payment
    - Wait for timeout period
    - Poll status endpoint
    - Eventually returns status="failed" with timeout error
    - Verify wallet NOT in database
    - Verify schedule NOT created

39. **`TestPaymentFlow_EndToEnd_HistoricalPayment`**
    - Insert matching transaction into database (before workflow starts)
    - POST /api/v1/wallet-assets
    - Workflow should complete immediately using lookback
    - Status endpoint quickly returns "completed"
    - Wallet registered

40. **`TestPaymentFlow_ExistingWallet_NoPayment`**
    - Register wallet manually (no payment)
    - POST /api/v1/wallet-assets for same wallet
    - Returns 200 OK (upsert, no payment required)
    - Wallet updated if poll_interval changed

41. **`TestPaymentFlow_PaymentGatewayDisabled`**
    - Config: PaymentGateway.Enabled = false
    - POST /api/v1/wallet-assets for new wallet
    - Returns 201 Created immediately
    - No workflow started
    - Wallet registered for free

42. **`TestPaymentFlow_ServiceWalletAutoRegistration`**
    - Start server with payment gateway enabled
    - Service wallet NOT in database initially
    - Server startup auto-registers service wallet
    - Service wallet is in database
    - Service wallet has schedule for polling

43. **`TestPaymentFlow_MultipleClients`**
    - Three clients register different wallets concurrently
    - Each receives unique invoice
    - Each sends payment
    - All three workflows complete successfully
    - All three wallets registered

44. **`TestPaymentFlow_DuplicateWorkflowID`**
    - Start workflow with ID "payment-registration:abc123"
    - Attempt to start another workflow with same ID
    - Temporal rejects duplicate (idempotent)
    - Client receives existing workflow ID

### Edge Case Tests

**File: `service/temporal/workflow_payment_edge_cases_test.go`**

45. **`TestEdgeCase_PaymentReceivedBeforeWorkflowStarts`**
    - User pays immediately after receiving invoice
    - Transaction appears in database
    - Workflow starts 5 seconds later
    - AwaitPayment activity uses lookback, finds transaction
    - Workflow completes without waiting

46. **`TestEdgeCase_MultiplePaymentsSameMemo`**
    - User accidentally sends payment twice with same memo
    - First payment completes workflow
    - Second payment arrives after workflow completed
    - Second payment is just a transaction (ignored by workflow)

47. **`TestEdgeCase_WorkflowRestartsAfterServerCrash`**
    - Start workflow
    - AwaitPayment activity is waiting
    - Simulate server crash (stop Temporal worker)
    - Restart worker
    - Activity resumes, SSE reconnects
    - Payment arrives
    - Workflow completes successfully

48. **`TestEdgeCase_PartialPayment`**
    - Required amount: 0.001 SOL (1000000 lamports)
    - User sends 0.0005 SOL (500000 lamports)
    - Matcher rejects (amount < required)
    - Workflow continues waiting

49. **`TestEdgeCase_PaymentToWrongNetwork`**
    - Invoice specifies mainnet
    - Service wallet is on mainnet
    - User sends payment on devnet
    - Service wallet (mainnet) never sees payment
    - Workflow times out

50. **`TestEdgeCase_PaymentWithoutMemo`**
    - User sends payment without memo
    - Matcher rejects (memo doesn't match)
    - Workflow continues waiting

51. **`TestEdgeCase_PaymentWithWrongMemo`**
    - Required memo: "forohtoo-reg:abc123"
    - User sends payment with memo: "forohtoo-reg:xyz789"
    - Matcher rejects
    - Workflow continues waiting

52. **`TestEdgeCase_RegistrationFailsAfterPayment_Retries`**
    - Payment succeeds
    - RegisterWallet activity fails with transient error
    - Activity retries (attempt 2) → fails
    - Activity retries (attempt 3) → succeeds
    - Workflow completes
    - Wallet registered

53. **`TestEdgeCase_RegistrationFailsAfterPayment_Exhausted`**
    - Payment succeeds (proof: transaction signature)
    - RegisterWallet activity fails all 3 attempts
    - Workflow fails with error
    - Result includes payment_signature (for support)
    - User can contact support with workflow_id

54. **`TestEdgeCase_ClientPollsStatusBeforeWorkflowStarts`**
    - Client receives workflow_id from 402 response
    - Client polls status endpoint before workflow actually starts
    - Returns 404 or "pending" (depending on timing)
    - Eventually workflow starts and status becomes available

55. **`TestEdgeCase_ClientRetriesRegistrationRequest`**
    - POST /api/v1/wallet-assets → receive 402 with workflow_id_1
    - Wait 5 seconds
    - POST /api/v1/wallet-assets again (same wallet)
    - Wallet still doesn't exist
    - Receive new 402 with workflow_id_2 (different invoice)
    - Two workflows running (user should use status endpoint instead)

56. **`TestEdgeCase_WorkflowTimesOutWhileRegisteringWallet`**
    - Payment arrives just before workflow timeout
    - AwaitPayment completes
    - RegisterWallet activity starts
    - Workflow timeout occurs during RegisterWallet
    - Activity is cancelled
    - Wallet may be partially registered (handle via retry)

### QR Code and Payment URL Tests

**File: `service/server/invoice_url_test.go`**

57. **`TestSolanaPayURL_SOL`**
    - Generate URL for SOL payment
    - Parse URL and verify parameters
    - Amount is correct (lamports → SOL)
    - Memo is URL-encoded
    - No spl-token parameter

58. **`TestSolanaPayURL_SPLToken`**
    - Generate URL for SPL token payment
    - Parse URL and verify parameters
    - spl-token parameter is present and correct
    - Amount is in token decimals

59. **`TestQRCode_Scannable`**
    - Generate QR code from payment URL
    - Decode QR code data (use QR library)
    - Verify decoded data matches payment URL

60. **`TestQRCode_Base64Encoding`**
    - QR code data is valid base64
    - Decodes to valid PNG image
    - PNG has reasonable dimensions (256x256)

### Client Library Tests

**File: `client/wallet_test.go`**

61. **`TestClient_Await_MatchingTransaction`**
    - Mock SSE stream with matching transaction
    - client.Await() returns immediately
    - Returned transaction has correct signature, amount, memo

62. **`TestClient_Await_NonMatchingTransactions`**
    - Mock SSE stream with non-matching transactions
    - Matcher rejects all of them
    - client.Await() continues waiting (or times out)

63. **`TestClient_Await_Timeout`**
    - Context with timeout
    - No matching transaction arrives
    - client.Await() returns context.DeadlineExceeded error

64. **`TestClient_Await_ContextCancelled`**
    - Context is cancelled while waiting
    - client.Await() returns context.Canceled error

65. **`TestClient_Await_LookbackFindsTransaction`**
    - Lookback period: 24h
    - Matching transaction exists 12h ago
    - client.Await() returns immediately with historical transaction

### Configuration Tests

**File: `service/config/config_payment_test.go`**

66. **`TestConfig_PaymentGateway_LoadFromEnv`**
    - Set environment variables
    - Load config
    - Verify all PaymentGateway fields are correct

67. **`TestConfig_PaymentGateway_MissingServiceWallet`**
    - Enabled=true but ServiceWallet not set
    - Validation fails with clear error

68. **`TestConfig_PaymentGateway_InvalidServiceWallet`**
    - ServiceWallet is not valid Solana address
    - Validation fails

69. **`TestConfig_PaymentGateway_NegativeFeeAmount`**
    - FeeAmount is negative
    - Validation fails

70. **`TestConfig_PaymentGateway_ZeroTimeout`**
    - PaymentTimeout is 0
    - Validation fails or uses default

### Database Tests (if we add any payment-related queries)

**File: `service/db/store_payment_test.go`**

71. **`TestStore_WalletExists_NotExists`**
    - Query wallet that doesn't exist
    - Returns false, nil error

72. **`TestStore_WalletExists_Exists`**
    - Create wallet
    - Query WalletExists
    - Returns true

### Service Wallet Auto-Registration Tests

**File: `service/server/server_payment_test.go`**

73. **`TestEnsureServiceWalletRegistered_NotExists`**
    - Service wallet NOT in database
    - Call ensureServiceWalletRegistered()
    - Service wallet is created
    - Schedule is created
    - Poll interval is 30s

74. **`TestEnsureServiceWalletRegistered_AlreadyExists`**
    - Service wallet already in database
    - Call ensureServiceWalletRegistered()
    - No error, idempotent
    - Wallet unchanged

75. **`TestEnsureServiceWalletRegistered_PaymentGatewayDisabled`**
    - PaymentGateway.Enabled = false
    - Call ensureServiceWalletRegistered()
    - No-op, returns immediately

76. **`TestEnsureServiceWalletRegistered_SPLToken`**
    - Service wallet asset type is spl-token
    - ATA is computed correctly
    - Schedule created with ATA

### Test Summary

**Total Tests: 76**

- Unit Tests: 16
- Activity Tests: 14
- Workflow Tests: 6
- Integration Tests: 8
- Edge Case Tests: 12
- QR Code/URL Tests: 4
- Client Library Tests: 5
- Configuration Tests: 5
- Database Tests: 2
- Service Wallet Tests: 4

### Test Execution Order

1. **Unit tests first** - fast, no dependencies
2. **Activity tests** - use mocks, moderately fast
3. **Workflow tests** - use Temporal test framework
4. **Integration tests** - slowest, use real components
5. **Edge case tests** - combination of above

### Test Helpers

Create helper functions to reduce boilerplate:

```go
// service/temporal/test_helpers.go

// MockPaymentClient for testing
type MockPaymentClient struct {
    AwaitFunc func(ctx context.Context, address, network string, lookback time.Duration, matcher func(*Transaction) bool) (*Transaction, error)
}

// TestWorkflowEnvironment creates a pre-configured Temporal test environment
func NewTestWorkflowEnvironment(t *testing.T) *testsuite.TestWorkflowEnvironment {
    // ...
}

// CreateTestInvoice generates an invoice for testing
func CreateTestInvoice() Invoice {
    // ...
}

// SimulatePayment inserts a matching transaction into test database
func SimulatePayment(t *testing.T, store *db.Store, invoice Invoice) *db.Transaction {
    // ...
}
```

### Coverage Goals

- **Unit tests**: 90%+ coverage
- **Activity tests**: 85%+ coverage
- **Workflow tests**: 80%+ coverage
- **Integration tests**: Critical paths covered
- **Edge cases**: All identified edge cases tested

### CI/CD Integration

- Run unit tests on every commit
- Run integration tests on PR
- Run edge case tests nightly
- Generate coverage reports
- Block merge if coverage drops

## Rollout Plan

### Phase 1: Implementation (Payment Gateway Disabled by Default)

1. **Add payment gateway configuration** (`service/config/config.go`)
   - Add `PaymentGatewayConfig` struct
   - Default `Enabled` to `false`
   - Environment variable bindings
   - Validation logic

2. **Implement invoice generation** (`service/server/invoice.go`)
   - `generatePaymentInvoice()` function
   - `buildSolanaPayURL()` for deep links
   - `generateQRCode()` for wallet scanning
   - Unit tests

3. **Implement Temporal workflow and activities** (`service/temporal/`)
   - `workflow_payment.go`: `PaymentGatedRegistrationWorkflow`
   - `activities_payment.go`: `AwaitPayment`, `RegisterWallet`
   - Activity needs `paymentClient *client.Client` dependency
   - Workflow and activity tests

4. **Update HTTP handlers** (`service/server/handlers.go`)
   - Modify `handleRegisterWalletAsset` to check payment gateway config
   - Add workflow start logic when payment required
   - Add `handleGetRegistrationStatus` endpoint
   - Handler tests

5. **Add service wallet auto-registration** (`service/server/server.go`)
   - `ensureServiceWalletRegistered()` function
   - Called during server startup
   - Tests

6. **Update Activities struct** (`service/temporal/activities.go`)
   - Add `paymentClient` field (type: `*client.Client`)
   - Update `NewActivities()` constructor
   - Update all activity instantiations in `cmd/worker/main.go`

7. **Update worker registration** (`cmd/worker/main.go`)
   - Register `PaymentGatedRegistrationWorkflow`
   - Pass payment client to Activities

8. **Write all tests** (76 tests enumerated above)
   - Unit tests
   - Activity tests
   - Workflow tests
   - Integration tests
   - Edge case tests

### Phase 2: Deploy to Production

**Note:** We're in early greenfield stage with no customers yet, so we can deploy directly without complex migration concerns.

1. **Deploy with payment gateway enabled**
   - Set environment variables:
     ```bash
     PAYMENT_GATEWAY_ENABLED=true
     PAYMENT_GATEWAY_SERVICE_WALLET=<mainnet-wallet-address>
     PAYMENT_GATEWAY_SERVICE_NETWORK=mainnet
     PAYMENT_GATEWAY_FEE_AMOUNT=1000000  # 0.001 SOL
     PAYMENT_GATEWAY_FEE_ASSET_TYPE=sol
     PAYMENT_GATEWAY_PAYMENT_TIMEOUT=24h
     PAYMENT_GATEWAY_MEMO_PREFIX=forohtoo-reg:
     ```
   - Deploy to production
   - Service wallet auto-registered on startup

2. **Manual verification on mainnet**
   - Register test wallet via API
   - Receive 402 with invoice and QR code
   - Scan QR code with wallet app
   - Send payment
   - Poll status endpoint
   - Verify workflow completes
   - Verify wallet registered and schedule created

3. **Set up monitoring**
   - Add Prometheus metrics (listed in checklist)
   - Configure alerts for failures and anomalies
   - Create Grafana dashboard for payment workflows
   - Monitor service wallet balance

4. **Ongoing monitoring**
   - Watch Temporal UI for workflow health
   - Monitor logs for errors
   - Track payment success rate
   - Verify service wallet receives payments

## Future Enhancements

1. **Multiple Payment Methods:**
   - Support different token types for fees (USDC, USDT, etc.)
   - Dynamic pricing based on USD equivalent

2. **Refunds:**
   - Automatically refund if registration fails
   - Requires private key management (complex)

3. **Subscription Model:**
   - Recurring payments for continued monitoring
   - Grace period before pausing wallet

4. **Payment Webhooks:**
   - Notify external systems when payment received
   - Integration with accounting systems

5. **Admin Dashboard:**
   - View pending payments
   - Manually complete/refund payments
   - Analytics (revenue, conversion rate, etc.)

## Open Questions

1. **Should we support partial payments?**
   - e.g., user sends 0.0005 SOL, then another 0.0005 SOL
   - Complexity: tracking multiple transactions per invoice
   - Decision: No for v1. Require full payment in single transaction.

2. **What happens to existing wallets when enabling payment gateway?**
   - Grandfather existing wallets (no payment required)
   - Only new registrations require payment
   - Existing wallets can update their settings for free

3. **Should service wallet poll interval be configurable?**
   - Could make it same as default wallet poll interval
   - Or fixed at 30s for faster payment detection
   - Decision: Fixed at 30s for better UX (faster payment confirmation)

4. **How long to retain completed workflow history?**
   - Temporal namespace retention policy
   - Decision: Use Temporal default (7-30 days), sufficient for support/debugging

## Conclusion

The payment gateway feature is architecturally sound and can be implemented using the existing client library without circular dependency issues. The **strongly recommended approach** is:

- **Temporal Workflow** (`PaymentGatedRegistrationWorkflow`)
- Activity uses `client.Await()` to monitor payments (dogfooding!)
- No new database tables needed (Temporal stores workflow state)
- Auto-register service wallet on startup
- Server restarts handled automatically by Temporal
- Built-in monitoring via Temporal UI
- QR codes and Solana Pay URLs for great UX

### Key Benefits

1. **Survives Restarts**: Temporal workflows persist across server restarts
2. **Simple Architecture**: No background goroutines, no pending_registrations table
3. **Built-in Monitoring**: View all pending payments in Temporal UI
4. **Retry Logic**: Activities automatically retry on transient failures
5. **Timeout Enforcement**: Workflow-level timeouts prevent indefinite blocking
6. **Dogfooding**: Service uses its own client library to monitor payments
7. **Type Safety**: Workflow and activity inputs/outputs are strongly typed

This design leverages the forohtoo service's own capabilities while maintaining operational simplicity and reliability.

## Implementation Checklist

Use this checklist to track implementation progress:

### Code Implementation

- [ ] **Configuration** (`service/config/config.go`)
  - [ ] Add `PaymentGatewayConfig` struct
  - [ ] Environment variable bindings
  - [ ] Validation logic
  - [ ] Tests (4 tests: defaults, validation, load from env, missing fields)

- [ ] **Invoice Generation** (`service/server/invoice.go`)
  - [ ] `Invoice` struct definition
  - [ ] `generatePaymentInvoice()` function
  - [ ] `buildSolanaPayURL()` function
  - [ ] `generateQRCode()` function (use `github.com/skip2/go-qrcode`)
  - [ ] Tests (5 tests: invoice generation, unique IDs, URL format, QR code)

- [ ] **Temporal Workflow** (`service/temporal/workflow_payment.go`)
  - [ ] `PaymentGatedRegistrationInput` struct
  - [ ] `PaymentGatedRegistrationResult` struct
  - [ ] `PaymentGatedRegistrationWorkflow` implementation
  - [ ] Tests (6 workflow tests)

- [ ] **Temporal Activities** (`service/temporal/activities_payment.go`)
  - [ ] `AwaitPaymentInput` and `AwaitPaymentResult` structs
  - [ ] `RegisterWalletInput` and `RegisterWalletResult` structs
  - [ ] `AwaitPayment()` activity with heartbeats
  - [ ] `RegisterWallet()` activity with rollback
  - [ ] Tests (14 activity tests)

- [ ] **Update Activities Struct** (`service/temporal/activities.go`)
  - [ ] Add `paymentClient *client.Client` field
  - [ ] Update `NewActivities()` constructor
  - [ ] Pass client instance when creating Activities

- [ ] **HTTP Handlers** (`service/server/handlers.go`)
  - [ ] Update `handleRegisterWalletAsset` for payment flow
  - [ ] Add `handleGetRegistrationStatus` endpoint
  - [ ] Tests (9 handler tests)

- [ ] **Server Setup** (`service/server/server.go`)
  - [ ] Add `ensureServiceWalletRegistered()` function
  - [ ] Call during server startup
  - [ ] Tests (4 service wallet tests)

- [ ] **Worker Registration** (`cmd/worker/main.go`)
  - [ ] Create payment client instance
  - [ ] Pass to Activities constructor
  - [ ] Register `PaymentGatedRegistrationWorkflow`

- [ ] **Server Routing** (`service/server/server.go`)
  - [ ] Add route for `GET /api/v1/registration-status/{workflow_id}`
  - [ ] Pass Temporal client to handler

- [ ] **UI Templates** (new section)
  - [ ] Create `templates/register-wallet.html` (main registration page)
  - [ ] Create `templates/partials/payment-dialog.html` (payment modal)
  - [ ] Create `templates/partials/payment-status.html` (status polling area)
  - [ ] Create `templates/partials/success-message.html` (success state)
  - [ ] Add htmx CDN to templates

- [ ] **UI Handlers** (`service/server/html_handlers.go`)
  - [ ] `handleRegisterWalletPage()` - render registration form
  - [ ] `handleRegisterWalletSubmit()` - process form submission, return HTML
  - [ ] `handleRegistrationStatusUI()` - return status partial for polling

- [ ] **UI Routes** (`service/server/server.go`)
  - [ ] `GET /ui/register-wallet` - show registration form
  - [ ] `POST /ui/wallet-assets` - submit registration (htmx)
  - [ ] `GET /ui/registration-status/{workflowID}` - status polling (htmx)
  - [ ] Add link to `/ui/register-wallet` from SSE client page

### UI/Template Implementation

Users should be able to register wallets via the web UI, not just the API. The implementation should follow existing patterns using htmx and Go templates, referencing patterns from [github.com/brojonat/g2i](https://github.com/brojonat/g2i).

#### Template Files

**File: `service/server/templates/register-wallet.html`**

Main wallet registration page with registration form.

**Key Elements:**
- Wallet registration form (address, network, asset type, poll interval)
- Submit button with htmx attributes
- Container for payment dialog (when required)
- Success/error message areas

**File: `service/server/templates/partials/payment-dialog.html`**

Reusable partial template for payment dialog/modal, shown when payment is required.

**Key Elements:**
- Payment invoice details (amount, recipient, memo)
- QR code image (base64-encoded)
- Solana Pay deep link
- Copy-to-clipboard buttons for address and memo
- Payment status polling area (auto-updates)
- Instructions for user

**File: `service/server/templates/partials/payment-status.html`**

Reusable partial for payment status display, replaces itself via polling.

**Statuses:**
- `pending`: Waiting for payment (show spinner)
- `completed`: Payment received, wallet registered (show success)
- `failed`: Payment timeout or registration error (show error)

#### HTMX Patterns

Reference patterns from g2i project:

**1. Form Submission**
```html
<form hx-post="/ui/wallet-assets"
      hx-target="#response-area"
      hx-swap="innerHTML"
      hx-indicator="#loading">
  <!-- form fields -->
  <button type="submit">Register Wallet</button>
</form>
<div id="loading" class="htmx-indicator">Submitting...</div>
```

**2. Payment Dialog Response**

When server returns 402 Payment Required, it should return HTML (not JSON) containing the payment dialog partial:
```html
<div id="payment-dialog" class="modal">
  <!-- Payment invoice with QR code -->
  <div hx-get="/ui/registration-status/{{ .WorkflowID }}"
       hx-trigger="every 2s"
       hx-target="#payment-status"
       hx-swap="outerHTML">
    <div id="payment-status">
      <!-- Status content (spinner, pending message) -->
    </div>
  </div>
</div>
```

**3. Status Polling**

The status partial replaces itself every 2 seconds:
```html
<div id="payment-status"
     hx-get="/ui/registration-status/{{ .WorkflowID }}"
     hx-trigger="every 2s"
     hx-swap="outerHTML">
  {{ if eq .Status "pending" }}
    <!-- Spinner and "Waiting for payment..." -->
  {{ else if eq .Status "completed" }}
    <!-- Success message, stop polling -->
  {{ else if eq .Status "failed" }}
    <!-- Error message, stop polling -->
  {{ end }}
</div>
```

**Key Pattern**: When status is `completed` or `failed`, the returned HTML should NOT include `hx-get` or `hx-trigger` attributes, which stops the polling.

**4. Success/Update Responses**

When payment gateway is disabled or wallet already exists:
```html
<div class="success-message">
  ✓ Wallet registered successfully!
  <a href="/ui/wallet-assets/{{ .Address }}">View wallet</a>
</div>
```

#### HTTP Handlers

**File: `service/server/html_handlers.go`**

Add handlers for UI routes:

```go
// handleRegisterWalletPage returns the registration form page
func handleRegisterWalletPage(renderer *TemplateRenderer) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        data := map[string]interface{}{
            "Networks":   []string{"mainnet", "devnet"},
            "AssetTypes": []string{"sol", "spl-token"},
            "SupportedTokenMints": cfg.SupportedTokenMints,
        }
        renderer.Render(w, "register-wallet.html", data)
    })
}

// handleRegisterWalletSubmit handles form submission via htmx
func handleRegisterWalletSubmit(
    store Store,
    scheduler Scheduler,
    temporalClient client.Client,
    cfg *PaymentGatewayConfig,
    logger *slog.Logger,
) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Parse form data
        address := r.FormValue("address")
        network := r.FormValue("network")
        assetType := r.FormValue("asset_type")
        tokenMint := r.FormValue("token_mint")
        pollInterval, _ := time.ParseDuration(r.FormValue("poll_interval"))

        // Same logic as API handler, but return HTML instead of JSON

        // Case 1: Payment gateway disabled
        // Return: success-message.html partial

        // Case 2: Wallet exists
        // Return: success-message.html partial

        // Case 3: New wallet + payment required
        // Return: payment-dialog.html partial with invoice
    })
}

// handleRegistrationStatusUI handles status polling via htmx
func handleRegistrationStatusUI(
    temporalClient client.Client,
    cfg *PaymentGatewayConfig,
    logger *slog.Logger,
) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        workflowID := chi.URLParam(r, "workflowID")

        // Query workflow status
        // Return payment-status.html partial with current status

        // If status is "completed" or "failed", omit hx-trigger to stop polling
    })
}
```

#### Routes

Add UI routes to server:

```go
// UI routes (return HTML)
r.Get("/ui/register-wallet", handleRegisterWalletPage(renderer))
r.Post("/ui/wallet-assets", handleRegisterWalletSubmit(store, scheduler, temporalClient, cfg, logger))
r.Get("/ui/registration-status/{workflowID}", handleRegistrationStatusUI(temporalClient, cfg, logger))

// Link from existing SSE client page
r.Get("/", handleSSEClientPage(renderer)) // Add link to /ui/register-wallet
```

#### CSS/Styling

Follow existing styles from `sse-client.html`:
- White cards with box-shadow
- Clean button styles (green for primary actions)
- Loading indicators with animation
- Modal overlay with centered content
- Responsive design for mobile

#### Payment Dialog Design

```
┌─────────────────────────────────────────────┐
│  Payment Required                      [X]  │
├─────────────────────────────────────────────┤
│                                             │
│  To register this wallet, please send:     │
│                                             │
│  Amount: 0.001 SOL                          │
│  To: FoRoHtOo...xyz123                      │
│  Memo: forohtoo-reg:abc-123-def             │
│                                             │
│  ┌───────────────────┐                      │
│  │                   │                      │
│  │    QR CODE        │                      │
│  │                   │                      │
│  └───────────────────┘                      │
│                                             │
│  [Copy Address]  [Copy Memo]                │
│  [Open in Wallet] (Solana Pay link)         │
│                                             │
│  ┌─────────────────────────────────────┐   │
│  │  ⟳ Waiting for payment...           │   │
│  │  (Auto-updating every 2 seconds)    │   │
│  └─────────────────────────────────────┘   │
│                                             │
│  Payment will expire in: 23:45:12           │
│                                             │
└─────────────────────────────────────────────┘
```

When payment is detected, the status area updates to:
```
┌─────────────────────────────────────┐
│  ✓ Payment received!                │
│  Wallet registered successfully     │
│  [View Transactions] [Close]        │
└─────────────────────────────────────┘
```

#### Implementation Notes

1. **Content Negotiation**: The same `handleRegisterWalletAsset` logic can be reused, but return HTML for UI routes and JSON for API routes. Check `Accept` header or use separate handler functions.

2. **QR Code**: Use the same `generateQRCode()` function, but embed as `<img src="data:image/png;base64,{{ .QRCodeData }}">` in the template.

3. **Solana Pay Link**: Generate `solana:...` URL and use as `<a href="{{ .PaymentURL }}">Open in Wallet</a>`. Mobile wallet apps will intercept this link.

4. **Polling Stop**: When status is `completed` or `failed`, the returned partial should NOT include `hx-get` or `hx-trigger`, which naturally stops polling.

5. **Error Handling**: If form validation fails, return the form partial with error messages highlighted (standard htmx pattern).

6. **Loading States**: Use `hx-indicator` to show loading spinners during form submission and status polling.

7. **Mobile-Friendly**: QR codes are easy to scan on desktop → mobile. Solana Pay links work on mobile devices.

#### Testing

- **Manual Testing**: Use browser to test UI flow
- **E2E Tests**: Use tools like Playwright or Selenium if desired
- **Template Rendering**: Test that templates render correctly with various data
- **HTMX Behavior**: Verify polling starts/stops correctly

#### References

- [g2i repository](https://github.com/brojonat/g2i) - HTMX patterns for forms, partials, and polling
- [htmx.org](https://htmx.org) - HTMX documentation
- Existing `sse-client.html` - Styling and layout patterns

### Testing

- [ ] **Unit Tests** (16 tests)
  - [ ] Invoice generation tests
  - [ ] Handler tests (payment gateway disabled/enabled)
  - [ ] Configuration tests

- [ ] **Activity Tests** (14 tests)
  - [ ] AwaitPayment activity tests (8 tests)
  - [ ] RegisterWallet activity tests (6 tests)

- [ ] **Workflow Tests** (6 tests)
  - [ ] Success path
  - [ ] Payment timeout
  - [ ] Registration failures with retries

- [ ] **Integration Tests** (8 tests)
  - [ ] End-to-end payment flow
  - [ ] Timeout scenario
  - [ ] Historical payment
  - [ ] Existing wallet (no payment)
  - [ ] Payment gateway disabled
  - [ ] Service wallet auto-registration
  - [ ] Multiple concurrent clients
  - [ ] Duplicate workflow ID

- [ ] **Edge Case Tests** (12 tests)
  - [ ] Payment before workflow starts
  - [ ] Multiple payments same memo
  - [ ] Server crash during workflow
  - [ ] Partial payment
  - [ ] Wrong network
  - [ ] Wrong/missing memo
  - [ ] Registration failures with retries
  - [ ] Client polling edge cases
  - [ ] Timeout during registration

- [ ] **QR Code/URL Tests** (4 tests)
- [ ] **Client Library Tests** (5 tests)
- [ ] **Database Tests** (2 tests)

### Documentation

- [ ] Update README.md with payment gateway feature
- [ ] Update API documentation (402 response format)
- [ ] Add environment variable documentation
- [ ] Create customer-facing payment flow documentation
- [ ] Create support runbook

### Deployment

- [ ] **Staging**
  - [ ] Deploy with payment gateway disabled
  - [ ] Verify existing functionality
  - [ ] Enable payment gateway on devnet
  - [ ] Manual end-to-end testing
  - [ ] Edge case testing

- [ ] **Production**
  - [ ] Deploy with payment gateway disabled
  - [ ] Monitor for 24-48 hours
  - [ ] Enable payment gateway on mainnet
  - [ ] Set up monitoring and alerts
  - [ ] Monitor for 1 week

### Monitoring & Observability

- [ ] Add Prometheus metrics:
  - [ ] `payment_workflows_started_total`
  - [ ] `payment_workflows_completed_total`
  - [ ] `payment_workflows_failed_total`
  - [ ] `payment_workflow_duration_seconds`
  - [ ] `payments_received_total`
  - [ ] `registrations_completed_total`
  - [ ] `service_wallet_balance_lamports` (gauge)

- [ ] Set up alerts:
  - [ ] High workflow failure rate
  - [ ] Long-running workflows
  - [ ] Service wallet balance drops
  - [ ] Registration failures after payment

- [ ] Create Grafana dashboards:
  - [ ] Payment workflow metrics
  - [ ] Service wallet monitoring
  - [ ] Registration success rates

### Total Progress: 0/76 tests + 20 implementation tasks
