# Subscription Overhaul: Historical + Live Streaming via SSE

## Goals

- Serve a timeline of transactions (historical window + live updates) over SSE
- Allow optional start_time and end_time to request a bounded time window
- Send historical transactions first (in chunks), then switch to live NATS stream
- Enable clients to deduplicate and render a time-series timeline using RxJS and D3

## High-level Flow

1. Client connects to SSE endpoint `/api/v1/stream/transactions[/{address}]`.
2. Server fetches historical transactions from the DB for a fixed window (specified by optional `lookback` parameter), optionally filter by wallet.
3. Server streams historical transactions as individual `transaction` events, ascending by time.
4. Server then switches to live streaming by consuming NATS JetStream (subject `txns.*` or `txns.{address}`) and emits `transaction` events one-by-one.
5. Client merges both streams into a single timeline, deduping by signature.

## Endpoints

- GET `/api/v1/stream/transactions` — all wallets
- GET `/api/v1/stream/transactions/{address}` — single wallet

Future (not implemented yet):

- Support `?start_time=ISO8601&end_time=ISO8601` for custom window selection.

## Event Types (SSE)

- `connected`: initial handshake
  - `{ wallet: string }`
- `transaction`: single transaction event (both historical and live)
  - `TransactionEvent`

## TransactionEvent Schema (subset)

```
{
  signature: string,
  slot: number,
  wallet_address: string,
  from_address?: string,
  amount: number,
  token_type?: string,
  memo?: string,
  timestamp: string,
  block_time: string,
  confirmation_status: string,
  published_at: string
}
```

## Backend Changes

- SQL: `ListTransactionsByTimeRange` (global), ordered ASC by `block_time`.
- Store: `ListTransactionsByTimeRange(ctx, start, end)`.
- SSE handler:
  - On connect: parse optional `lookback` duration parameter.
  - Fetch from DB, filter by address if provided.
  - Stream each historical transaction individually as `transaction` events.
  - Create NATS consumer with `DeliverNewPolicy` and forward live `transaction` events.

## Client Design (RxJS + D3) — to be implemented

- RxJS
  - `connected$`: handshake events
  - `transactions$`: all `transaction` events (both historical and live)
  - `timeline$ = transactions$.pipe(scan(dedupBySignature), shareReplay(1))`
  - Dedup keys: `signature`; Order by `block_time` (fallback `published_at`).
  - Reconnection handling with exponential backoff; resume dedup from cache.
- D3 Timeline
  - Time scale on X from requested `start_time..end_time`.
  - Circles/marks for transactions; color by `confirmation_status`.
  - Tooltip on hover with signature, amount (SOL), memo, token type.
  - Zoom/pan (later): re-render domain and marks.

## Chunking & Performance

- Chunk size 200 (tunable via env).
- Flush after each chunk. Keepalive comment every 10s.
- Historical fetch timeout: 10s to avoid blocking.

## Reconnections

- Client should cache last received `block_time` and signatures.
- On reconnect, request same window or shift forward.
- Server currently always returns last 24h (placeholder); will add query params later.

## Open Questions / Next Steps

- Add query params `start_time` and `end_time` (ISO8601) and validate.
- Per-wallet historical fetch query for efficiency (avoid filtering in app).
- Backfill longer history with pagination.
- Security: rate limits and auth (API keys?) for public endpoints.
- Metrics: count of events sent, per-connection durations, dropped messages.
