# LEARNINGS

Hard-won knowledge. Each entry: what happened, why it was surprising, how to avoid it.

## Helius webhook API hostname

**What happened.** First webhook integration attempt used `https://api.helius.dev/v0` as the
base URL. DNS doesn't resolve — the server would crash at startup (`EnsureWebhooks` fails)
before any traffic is served. Caught only because we ran `forohtoo helius diff` against
prod env locally as a pre-deploy check.

**Why it was surprising.** `helius.dev` looks plausible (it's their corporate site), and the
client compiles + tests pass with the wrong URL because the unit tests mock the HTTP layer.
Nothing in the build pipeline catches a typo'd third-party hostname.

**How to avoid it.**
- Canonical webhook API host (per current docs): `https://api-mainnet.helius-rpc.com/v0`.
- Legacy alias `https://api.helius.xyz/v0` also works but is undocumented; don't rely on it.
- For any third-party API, run a real network call (CLI smoke test, integration test
  with `-tags=live`, or curl) before deploying. Mocked tests cannot catch a wrong hostname.
- The `forohtoo helius list` / `helius diff` commands serve as that smoke test for Helius
  specifically — run them with prod env loaded before every deploy that touches the
  webhook integration.
