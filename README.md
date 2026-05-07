# Solana Wallet Payment Service

A Go service and client library for streaming Solana wallet transactions to
your applications. Helius webhooks push every relevant transaction into the
service in real time; clients subscribe over SSE.

## Architecture

```
        Helius webhook (push)
               │
               ▼
   ┌──────────────────────────┐
   │       HTTP Server        │
   │                          │
   │   /webhooks/helius ──┐   │
   │                      │   │
   │      TimescaleDB ◀───┤   │
   │                      ▼   │
   │      NATS  ──▶  /stream  │
   └──────────────────────────┘
                              │ SSE
                              ▼
                      ┌──────────────┐
                      │   Clients    │
                      │  - Await()   │
                      │  - CRUD      │
                      └──────────────┘
```

**Key design decisions:**

- **Helius is the only ingestion path.** No RPC polling.
- **NATS is internal**, never exposed to clients.
- **Clients only need HTTP/SSE** — no NATS client library required.
- **Temporal is optional**, used only for the payment-gated registration
  workflow when `PAYMENT_GATEWAY_ENABLED=true`. Its worker runs in-process
  inside the server.

## How It Works

1. Client registers a wallet (and asset: SOL or SPL token) via REST.
2. Server adds the address (or its associated token account for SPL) to a
   single Helius enhanced webhook.
3. Helius posts every successful transaction touching the watched addresses
   to `/api/v1/webhooks/helius`. The handler authenticates the request,
   parses the payload, writes to TimescaleDB, and publishes events to NATS.
4. Clients subscribe to transactions over SSE
   (`/api/v1/stream/transactions/{address}`).

## Components

### HTTP Server (`cmd/server`)

The single deployable process. Responsibilities:

- Wallet management (register / unregister / list)
- Helius webhook receiver
- SSE transaction streaming
- (Optional) in-process Temporal worker for the payment-gated registration
  workflow

### Client Library (`client/`)

- `RegisterAsset` / `UnregisterAsset` / `Get` / `List`
- `Await(ctx, wallet, network, lookback, matcher)` — block until a
  transaction matching your custom matcher arrives over SSE, with optional
  historical lookback.

### CLI (`cmd/forohtoo`)

- `db list-wallets` / `db get-wallet` / `db list-transactions`
- `wallet add` / `wallet list` / `wallet get` / `wallet await`
- `nats subscribe` / `nats smoke-test` / `nats inspect-stream`
- `sse stream`
- `server health`

## API

### Wallet Management

- `POST /api/v1/wallet-assets` — register a wallet+asset.
- `GET /api/v1/wallet-assets` — list all.
- `GET /api/v1/wallet-assets/{address}?network=` — list assets for one wallet.
- `DELETE /api/v1/wallet-assets/{address}?network=&asset_type=&token_mint=`

### Webhook

- `POST /api/v1/webhooks/helius` — receives Helius pushes.
  Authenticated by the shared secret in `HELIUS_WEBHOOK_AUTH_TOKEN`.

### SSE

- `GET /api/v1/stream/transactions/{address}?network=`
- `GET /api/v1/stream/transactions?network=` — all wallets
- `?lookback=24h` — replay historical events before live streaming

### Payment Gateway (when enabled)

- `POST /api/v1/wallet-assets` for an unregistered wallet returns `402` with
  an invoice and a `workflow_id`.
- `GET /api/v1/registration-status/{workflow_id}` — poll status.

## Required Configuration

```bash
DATABASE_URL=postgres://...
NATS_URL=nats://localhost:4222

USDC_MAINNET_MINT_ADDRESS=EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v
USDC_DEVNET_MINT_ADDRESS=4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU

HELIUS_API_KEY=...
HELIUS_WEBHOOK_URL=https://your.host/api/v1/webhooks/helius
HELIUS_WEBHOOK_AUTH_TOKEN=Bearer your-shared-secret

# Optional payment gateway
PAYMENT_GATEWAY_ENABLED=false
TEMPORAL_HOST=localhost:7233
TEMPORAL_NAMESPACE=default
TEMPORAL_TASK_QUEUE=forohtoo-payment-gateway
```

See `.env.server.example` for the full list.

## Running Locally

```bash
make docker-up                # postgres, nats, (temporal)
make db-migrate-up
make build
make run-server
```

The server is the only process you need to run. It will:

- Ensure a Helius webhook exists at `HELIUS_WEBHOOK_URL`.
- Sync all registered wallet addresses to the webhook on startup.
- Start the in-process payment-gateway worker if enabled.

For hot-reload development, `make run-dev-server` (uses Air).

## Testing

```bash
# unit tests
make test

# DB tests (requires postgres-test on :15433)
docker compose up -d postgres-test
TEST_DATABASE_URL="postgres://postgres:postgres@localhost:15433/forohtoo_test?sslmode=disable" \
  RUN_DB_TESTS=1 go test ./...
```

## Deployment

The service is a single image with a single binary. Kubernetes manifests in
`k8s/prod/`. `make k8s-apply-kustomize` applies them with `kustomize`. There
is no worker deployment — `PaymentGatedRegistrationWorkflow` is processed
in-process by the server.

## Memo Convention

For payment-gating, the memo of the funding transaction must match the
invoice memo (commonly `forohtoo-reg:<wallet_address>`). For application
gating via `client.Await`, you can pass any matcher closure — match on
amount, signature, parsed memo JSON, or arbitrary logic.

## License

(To be determined)
