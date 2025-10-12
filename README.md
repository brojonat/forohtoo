# Solana Wallet Payment Service

A Go-based service and client library for polling Solana wallets and integrating payment verification into Temporal workflows. This system decouples Solana RPC polling from client applications, enabling efficient payment tracking across multiple services without rate limit concerns.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  BACKEND SERVICE                                 │
│                                                  │
│  ┌─────────────┐     NATS       ┌─────────────┐  │
│  │   Worker    │───────────────▶│ HTTP Server │  │
│  │             │  (internal)    │             │  │
│  │ - Poll RPC  │                │ - REST API  │  │
│  │ - Write DB  │                │ - SSE       │  │
│  │ - Pub NATS  │                │             │  │
│  └─────────────┘                └─────────────┘  │
│         │                              │         │
│         │                              │         │
│    TimescaleDB                         │         │
│   (persistence)                        │         │
└────────────────────────────────────────┼─────────┘
                                         │
                                         │ HTTP/SSE
                                         ▼
                              ┌─────────────────────┐
                              │  WEB CLIENTS        │
                              │  - REST API         │
                              │  - SSE Streams      │
                              │  - Browser/Mobile   │
                              └─────────────────────┘
```

**Key Design Decisions:**

- **NATS is internal**: Not exposed to clients, simplifying security
- **SSE for streaming**: Clients use Server-Sent Events over HTTP
- **HTTP-only clients**: No need for NATS client libraries
- **CLI for ops**: Direct NATS access for debugging/monitoring

## Key Components

### Backend Service

The backend service runs independently and handles:

- **Wallet Polling**: Uses Temporal schedules to poll configured Solana wallets at specified intervals (e.g., every 30 seconds)
- **Transaction Storage**: Writes all transactions to TimescaleDB for long-term storage and analytics
- **Real-time Publishing**: Publishes transaction events to JetStream for real-time client consumption
- **Wallet Management**: Exposes NATS RPC endpoints for adding/removing/listing watched wallets

### Client Library

The Go client library provides:

- **Wallet Management**: Add/remove wallets to poll via NATS RPC
- **Real-time Updates**: Subscribe to transaction streams via JetStream
- **Catch-up Support**: Replay missed transactions after disconnect (JetStream feature)
- **Memo Parsing**: Parse transaction memos locally (supports custom formats)
- **Workflow Integration**: Block/unblock Temporal workflows based on payment verification

## Data Flow

```
Solana Poll → Write to TimescaleDB → Publish to JetStream
                      ↓                        ↓
              Long-term storage        Real-time clients
              Analytics queries        Catch-up on restart
              (months/years)           (days/weeks retention)
```

### Why Both TimescaleDB and JetStream?

**TimescaleDB:**

- Long-term retention (months to years)
- Complex analytics queries
- Aggregations (daily volume, top payers, token distributions)
- Hypertables for efficient time-series queries
- Continuous aggregates for rollups

**JetStream:**

- Recent transaction stream (configurable, typically 7-30 days)
- Real-time delivery to clients
- Client catch-up after disconnect/restart
- Replay capability for new subscribers
- No direct database access needed by clients

## NATS Subject Design

### Request/Reply (Management)

```
wallet.add      → {address, poll_interval}
wallet.remove   → {address}
wallet.list     → reply with watched wallets
wallet.status   → {address} → last poll time, txn count, etc
```

### Pub/Sub (Transaction Stream)

```
txns.{wallet_address}
```

Backend publishes raw transaction data including:

- Signature
- Slot number
- Amount
- Token type
- Memo (stored as text, unparsed)
- Timestamp
- Block time
- Confirmation status

Clients subscribe to specific wallet addresses and parse memos locally.

## Transaction Memo Format

For workflow-related payments, the memo field should contain JSON:

```json
{
  "workflow_id": "payment-workflow-abc123",
  "metadata": {
    "order_id": "12345",
    "custom_field": "value"
  }
}
```

The `workflow_id` field is used by clients to match payments to Temporal workflows.

## Use Cases

- **Payment Gating**: Block Temporal workflows until payment is received
- **Multi-tenant Payment Tracking**: Multiple services poll their own wallets without interfering
- **Payment Analytics**: Long-term storage enables reporting and analysis
- **Audit Trails**: Complete transaction history in TimescaleDB
- **Real-time Notifications**: Immediate payment detection via JetStream

## Configuration

### Backend Dependencies

- Solana RPC endpoint(s)
- NATS server with JetStream enabled
- TimescaleDB instance
- Temporal server (for scheduling polls)

### Client Dependencies

- NATS server connection
- Temporal client (optional, for workflow integration)

## Getting Started

### Running Locally

Start all required services:

```bash
# Start Postgres, NATS, and Temporal
make docker-up

# Wait for services to be ready (Temporal takes ~60 seconds)
sleep 60

# Run database migrations
make db-migrate-up

# Set required environment variables (or copy .env.example to .env)
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/forohtoo?sslmode=disable"
export SOLANA_RPC_URL="https://api.mainnet-beta.solana.com"
```

The system consists of **two separate processes** that run independently:

#### 1. HTTP Server (API)

Handles wallet management via REST API:

```bash
make run-server
# or
./bin/server
```

Listens on `:8080` by default. Provides endpoints for:

**Wallet Management:**

- `POST /api/v1/wallets` - Register a wallet for polling
- `GET /api/v1/wallets` - List all registered wallets
- `GET /api/v1/wallets/{address}` - Get wallet details
- `DELETE /api/v1/wallets/{address}` - Unregister a wallet

**Transaction Streaming (SSE):**

- `GET /api/v1/stream/transactions/{address}` - Stream transactions for a specific wallet
- `GET /api/v1/stream/transactions` - Stream transactions for all wallets

The SSE endpoints provide real-time transaction streams over HTTP. See `examples/sse-client.html` for a browser-based demo.

#### 2. Temporal Worker

Processes scheduled wallet polling workflows:

```bash
make run-worker
# or
./bin/worker
```

The worker executes `PollWalletWorkflow` on schedule, which:

- Polls Solana RPC for new transactions
- Writes transactions to TimescaleDB
- Updates wallet poll time

#### Running Both Together

For local development, you can use tmux or separate terminal windows:

```bash
# Terminal 1
make run-server

# Terminal 2
make run-worker
```

For production, deploy as separate Kubernetes deployments to scale independently:

- Scale API servers based on request load
- Scale workers based on number of wallets being polled

Logs are output as structured JSON to stderr.

### Kubernetes Deployment

The service uses Kustomize for Kubernetes deployments with separate server and worker manifests.

#### Prerequisites

1. **Docker Registry**: Push images to your registry
2. **Environment Files**: Create production environment files from examples:
   ```bash
   cp .env.server.example .env.server.prod
   cp .env.worker.example .env.worker.prod
   # Edit files with production values
   ```
3. **Image Pull Secret**: Create `regcred` secret for private registries:
   ```bash
   kubectl create secret docker-registry regcred \
     --docker-server=your-registry.com \
     --docker-username=your-username \
     --docker-password=your-password
   ```

#### Deployment Process

**Option 1: Full Deployment (Build + Push + Apply)**

```bash
# Set variables
export DOCKER_REPO="your-registry.com/your-org"
export GIT_COMMIT_SHA=$(git rev-parse --short HEAD)

# Build, push, and deploy
make deploy
```

**Option 2: Using Kustomize (Recommended for Production)**

```bash
# Build and push image
export DOCKER_REPO="your-registry.com/your-org"
export GIT_COMMIT_SHA=$(git rev-parse --short HEAD)
make docker-build-tag docker-push

# Update k8s manifests with new image tags
sed -i "s|{{DOCKER_REPO}}|${DOCKER_REPO}|g" k8s/prod/*.yaml
sed -i "s|{{GIT_COMMIT_SHA}}|${GIT_COMMIT_SHA}|g" k8s/prod/*.yaml

# Apply with kustomize (loads secrets from .env files)
make k8s-apply-kustomize
```

**Option 3: Direct kubectl Apply**

```bash
export DOCKER_REPO="your-registry.com/your-org"
export GIT_COMMIT_SHA=$(git rev-parse --short HEAD)
make k8s-apply
```

#### Monitoring and Management

```bash
# Check deployment status
make k8s-status

# View logs
make k8s-logs-server
make k8s-logs-worker

# Restart deployments
make k8s-restart-server
make k8s-restart-worker

# Delete all resources
make k8s-delete
```

#### Architecture

The deployment consists of:

- **Server Deployment** (`forohtoo-server`):

  - 1 replica (scale up based on API load)
  - Exposes ClusterIP service on port 80 → 8080
  - Health checks on `/health` endpoint
  - Resources: 128Mi-512Mi RAM, 100m-500m CPU

- **Worker Deployment** (`forohtoo-worker`):
  - 1 replica (scale up based on wallet count)
  - No exposed service (processes Temporal tasks)
  - Resources: 256Mi-1Gi RAM, 200m-1000m CPU

Both deployments use:

- **Same Docker image** with different commands (`/server` vs `/worker`)
- **Separate secrets** from `.env.server.prod` and `.env.worker.prod`
- **Image pull secret** (`regcred`) for private registries

**Scaling Strategies:**

- **API Server**: Scale based on HTTP request rate, CPU, or memory
- **Worker**: Scale based on number of active wallets or Temporal task queue depth
- Temporal handles task distribution across multiple worker instances automatically

### Transaction Streaming

#### Browser/Web Clients (SSE)

For production applications, use the HTTP Server-Sent Events endpoints:

```javascript
// Stream transactions for a specific wallet
const eventSource = new EventSource(
  "http://localhost:8080/api/v1/stream/transactions/YOUR_WALLET"
);

eventSource.addEventListener("connected", (e) => {
  console.log("Connected:", JSON.parse(e.data));
});

eventSource.addEventListener("transaction", (e) => {
  const txn = JSON.parse(e.data);
  console.log("Transaction:", txn);
  // Handle transaction event
});

// Stream all wallet transactions
const allSource = new EventSource(
  "http://localhost:8080/api/v1/stream/transactions"
);
```

See `examples/sse-client.html` for a complete browser-based demo.

**Benefits of SSE:**

- Standard browser API (EventSource)
- Works with standard HTTP infrastructure
- Automatic reconnection
- No NATS client library needed
- Simple authentication (HTTP headers/cookies)

#### CLI Tool (SSE)

For debugging and testing without a browser, use the CLI to stream SSE to stdout:

```bash
# Stream transactions for a specific wallet (human-friendly output)
forohtoo sse stream YOUR_WALLET_ADDRESS

# Stream all wallets
forohtoo sse stream

# JSON output (one transaction per line)
forohtoo sse stream YOUR_WALLET_ADDRESS --json

# Use custom server URL
forohtoo sse stream YOUR_WALLET_ADDRESS --server http://production-server:8080
```

**Benefits:**

- No browser required for debugging
- Same SSE endpoints as browser clients
- Human-friendly or JSON output
- Easy integration with scripts and automation

### Blocking Payment Verification (Await)

The **client.Await()** method is the core feature for payment gating in Temporal workflows. It connects to SSE and blocks until a transaction matching your custom criteria arrives, enabling workflows to pause until payment is confirmed.

**How It Works:**

- Connects to SSE stream for a wallet
- Calls your matcher function on every transaction
- Returns when matcher returns `true`
- Robust through proxies (SSE auto-reconnects)
- Client-side filtering (any logic you want)

**Go Client Library:**

```go
import "github.com/brojonat/forohtoo/client"

// Create client
cl := client.NewClient("http://localhost:8080", nil, logger)

// Block until transaction matching your criteria arrives
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

txn, err := cl.Await(ctx, walletAddress, func(txn *client.Transaction) bool {
    // Custom matching logic - any condition you want!

    // Example 1: Match by workflow_id in memo
    return strings.Contains(txn.Memo, "payment-workflow-123")

    // Example 2: Match by minimum amount
    // return txn.Amount >= 1000000000 // 1 SOL

    // Example 3: Match by signature
    // return txn.Signature == expectedSignature

    // Example 4: Complex logic
    // if txn.Amount < minimumAmount {
    //     return false
    // }
    // return containsWorkflowID(txn.Memo, expectedID)
})

if err != nil {
    return fmt.Errorf("payment not received: %w", err)
}

// Payment confirmed! Continue workflow
log.Printf("Payment received: %s (%.4f SOL)", txn.Signature, float64(txn.Amount)/1e9)
```

**CLI Tool:**

```bash
# Block until transaction with workflow_id arrives (using jq filter)
forohtoo wallet await --must-jq '. | contains({workflow_id: "payment-workflow-123"})' \
  YOUR_WALLET

# Block until specific signature arrives
forohtoo wallet await --signature SIG_HERE YOUR_WALLET

# JSON output for automation
forohtoo wallet await --must-jq '. | contains({workflow_id: "xyz"})' \
  --json \
  YOUR_WALLET

# Custom timeout
forohtoo wallet await --must-jq '. | contains({workflow_id: "xyz"})' \
  --timeout 10m \
  YOUR_WALLET
```

**Use Case - Temporal Workflow:**

```go
// Workflow activity that waits for payment
func WaitForPaymentActivity(ctx context.Context, walletAddr string, workflowID string) (*client.Transaction, error) {
    cl := client.NewClient(serverURL, nil, logger)

    // Block until transaction with matching workflow_id arrives
    return cl.Await(ctx, walletAddr, func(txn *client.Transaction) bool {
        return strings.Contains(txn.Memo, workflowID)
    })
}

// Workflow
func PaymentGatedWorkflow(ctx workflow.Context, order Order) error {
    // Generate unique workflow ID for this payment
    workflowID := workflow.GetInfo(ctx).WorkflowExecution.ID

    // Display payment instructions to user
    // Memo should include: {"workflow_id": "payment-workflow-abc123"}

    // Block workflow until payment received (with timeout)
    activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 15 * time.Minute,
    })

    var txn *client.Transaction
    err := workflow.ExecuteActivity(activityCtx, WaitForPaymentActivity,
        order.WalletAddress, workflowID).Get(ctx, &txn)
    if err != nil {
        return fmt.Errorf("payment timeout or failed: %w", err)
    }

    // Payment confirmed! Continue with order fulfillment
    return processOrder(ctx, order, txn)
}
```

**Benefits:**

- **Flexible Filtering**: Callback can implement ANY matching logic
- **SSE-Based**: Robust through proxies and load balancers
- **Auto-Reconnect**: SSE handles connection drops automatically
- **Client-Side Control**: See all transactions, decide what matches
- **Context Timeout**: Standard Go context for timeout handling
- **Simple Integration**: Just HTTP/SSE - no NATS client required

**Security Considerations:**

- Consider adding authentication (JWT, API keys) to SSE endpoints
- Use HTTPS in production
- Validate memo format to prevent injection attacks
- Monitor SSE connection count for abuse

#### CLI Tool (NATS Direct)

For debugging and operations, the CLI connects directly to NATS:

```bash
# Subscribe to a specific wallet
forohtoo nats subscribe DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK

# Subscribe with JSON output
forohtoo nats subscribe DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK --json

# Create a durable consumer (survives restarts)
forohtoo nats subscribe DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK --durable
```

#### Smoke Testing

Run an end-to-end smoke test to verify the system is working:

```bash
# Run smoke test with a known busy wallet (Pump.fun bonding curve)
forohtoo nats smoke-test

# Use a custom wallet and timeout
forohtoo nats smoke-test --wallet YOUR_WALLET --timeout 60s

# JSON output for automation
forohtoo nats smoke-test --json
```

The smoke test:

1. Connects to NATS JetStream
2. Subscribes to transaction events for a busy wallet
3. Waits for transaction events (default: 30s)
4. Reports success/failure

This verifies that the entire pipeline is working: Solana → Worker → TimescaleDB → NATS → CLI

#### Other Commands

```bash
# Inspect JetStream stream status
forohtoo nats inspect-stream

# Database inspection
forohtoo db list-wallets
forohtoo db list-transactions WALLET_ADDRESS

# Temporal schedule management
forohtoo temporal list-schedules
forohtoo temporal describe-schedule WALLET_ADDRESS

# Server health check
forohtoo server health
```

See `forohtoo --help` for all available commands.

### Running Tests

```bash
# Run unit tests (no external dependencies)
make test

# Run integration tests (requires docker services)
make test-integration
```

See [TESTING.md](./TESTING.md) for detailed testing instructions.

### Worked Example: Monitoring IncentivizeThis Escrow Wallet for USDC

- First, run the server and worker locally
- Then, run the SSE client in your browser
- You shouldn't see any transactions flowing in yet (provided the server isn't subscribed to any wallets).
- Issue the following client command to subscribe to the wallet:

```bash
forohtoo wallet add DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK
```

- You should start to see transactions flowing in if by some miracle IncentivizeThis is paying out. If traffic is low, you can just send yourself some USDC and you should see the transaction in your browser. Cool, you should be convinced the system is working.
- Now, create a bounty, but don't fund it yet. Note the workflow_id, and keep that QR code handy. Let's say for example's sake, the bounty will be for 0.42 USDC, and the workflow_id is "some-id".
- Next, run the `await` command to block until a transaction matching the IncentivizeThis criteria arrives:

```bash
forohtoo wallet await --usdc-amount-equal 0.42 \
  --must-jq '. | contains({workflow_id: "some-id"})' \
  DYw8jCTfwHNRJhhmFcbXvVDTqWMEVFBX6ZKUmG5CNSKK
```

This should block until someone sends 0.42 USDC to the IncentivizeThis escrow wallet with a memo that can be parsed as JSON and contains a workflow_id field that matches the arbitrary workflow_id we provided. Now, you can use the QR code to fund the bounty. This transaction will be detected by the `await` command, which should now unblock!

**How it works:**

- `--usdc-amount-equal 0.42` checks that the transaction amount equals exactly 0.42 USDC (420000 lamports)
- `--must-jq '. | contains({workflow_id: "some-id"})'` runs a jq filter on the memo (parsed as JSON) that checks if it contains a workflow_id field matching "some-id"
- You can specify multiple `--must-jq` flags - ALL must evaluate to true for the transaction to match
- The matcher function is a closure that combines all these conditions with AND logic

## License

(To be determined)

```

```
