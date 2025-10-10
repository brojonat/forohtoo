# Solana Wallet Payment Service

A Go-based service and client library for polling Solana wallets and integrating payment verification into Temporal workflows. This system decouples Solana RPC polling from client applications, enabling efficient payment tracking across multiple services without rate limit concerns.

## Architecture

```
┌──────────────────────────────────────────┐
│  BACKEND SERVICE                          │
│  - NATS Request/Reply: wallet mgmt       │
│  - Temporal Schedule: poll wallets       │
│  - NATS Publish: txn updates             │
│  - TimescaleDB: persistent storage       │
└──────────────────────────────────────────┘
         │
         │ NATS + JetStream
         ▼
┌─────────────────────────────────────────┐
│  CLIENT LIBRARY (Go)                     │
│  - Request/Reply: manage wallets         │
│  - JetStream Subscribe: txn updates      │
│  - Parse memos locally                   │
│  - Unblock Temporal workflows            │
└──────────────────────────────────────────┘
```

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

(Coming soon: installation and quick start guide)

## License

(To be determined)
