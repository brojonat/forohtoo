# Bug Report: Transaction Not Being Ingested

## Summary
Transaction sent to wallet `CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv` for 0.1 USDC is visible on Solana blockchain but not being ingested by the forohtoo service.

## Transaction Details
- **Wallet**: `CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv` (mainnet)
- **Token Account (ATA)**: `FNTMqcBNDxBDsm25zsj68ii7R6F451kXVyitxPSAET9g`
- **Signature**: `5y9cmsBAZ7BAjtm7hYxiy3XyDY6z7VW7AqU8xheHex1LtfX2RuuqYmL4CZ9Ve2zEyZ4z1Qj9QJx8k1hEivg6BnSH`
- **Amount**: 0.1 USDC (100000 micro-USDC)
- **Block Time**: 2025-11-17 16:27:36 UTC
- **Slot**: 380710540
- **Status**: Finalized
- **Memo**: `bounty-b0121622-0233-4d0b-88a3-74a2317ddc5d`

## Verification

### Transaction EXISTS on blockchain
```bash
curl -s -X POST https://api.mainnet-beta.solana.com -H "Content-Type: application/json" -d '{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "getSignaturesForAddress",
  "params": ["FNTMqcBNDxBDsm25zsj68ii7R6F451kXVyitxPSAET9g", {"limit": 3}]
}' | jq '.result[0]'
```
Returns the transaction signature as the most recent transaction (from Nov 17, 2025).

### Database is EMPTY
```bash
kubectl exec forohtoo-worker-dc969558b-7k4g5 -- /forohtoo db list-transactions --wallet CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv --limit 100
```
Returns: `[]`

### Wallet IS registered and being polled
```bash
kubectl exec forohtoo-worker-dc969558b-7k4g5 -- /forohtoo db list-wallets | grep CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv
```
Shows wallet is active, last polled at `2025-11-17T16:28:21Z` (45 seconds AFTER transaction was sent).

### Worker reports 0 transactions on every poll
From kubectl logs:
```
2025-11-17T20:07:09Z ... "wallet_address":"CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv" ... "transaction_count":0
2025-11-17T20:07:09Z ... "no new transactions found"
```

## Root Causes Identified

### 1. Missing LastSignature Tracking (service/temporal/workflow.go:87)
```go
// TODO: Track last signature in workflow state or query from DB
var lastSignature *string  // <-- ALWAYS NIL!
```

The `lastSignature` is never set, so every poll fetches the most recent 20 transactions without any checkpoint. However, this alone shouldn't cause the issue since the transaction is the MOST RECENT one and should be in every poll.

### 2. GetSignaturesForAddress Returns Empty Array (Root Cause - Unconfirmed)
The worker logs show `transaction_count: 0` with no errors, which means `GetSignaturesForAddress` RPC call is succeeding but returning an empty array. This is inconsistent with manual RPC queries which return the transaction.

**Possible causes:**
- RPC client configuration difference (commitment level, encoding, etc.)
- Timing issue (transaction not finalized when polled at 16:28:21?)
- Bug in gagliardetto/solana-go library or our usage of it
- Silent failure or filtering in the Solana client code

## Investigation Findings

### No errors in logs
```bash
kubectl logs -l app=forohtoo-worker --since=2h | grep -i "error\|fail\|rate"
```
Returns no results - no rate limiting, no errors.

### Debug logs from Solana client not appearing
The debug log message "fetched transaction signatures" from `service/solana/client.go:102` never appears in production logs for ANY wallet, suggesting either:
- Logger not configured correctly for that component
- Log level filtering
- Logs are present but we're not seeing them

### Deployed version is current
Image tag: `a5a588b88dfbf4b70f3c0ffac90c3bb8e696d605` (latest commit on main)

### RPC configuration matches
Worker uses `https://api.mainnet-beta.solana.com` (same as manual queries).

## Next Steps

### Immediate Debug Actions
1. **Add detailed RPC logging**: Add explicit logging before/after GetSignaturesForAddress to see what's being returned
2. **Test with Go script**: Create a standalone Go test that uses the same gagliardetto/solana-go client with identical parameters
3. **Check commitment level**: Verify if gagliardetto/solana-go is using a specific commitment level that might filter out transactions
4. **Manual trigger**: Try manually triggering a poll via Temporal UI or CLI to force a fresh poll

### Code Fixes Needed
1. **Implement lastSignature tracking** (workflow.go:87):
   - Store last seen signature in workflow state or query from database
   - Use it to paginate through transactions properly

2. **Add comprehensive logging**:
   - Log GetSignaturesForAddress parameters and response
   - Log each transaction as it's processed
   - Add metrics for RPC response sizes

3. **Add alerts**:
   - Alert when a wallet hasn't seen transactions in N hours (but blockchain shows activity)
   - Alert when RPC returns unexpected empty arrays

## Reproduction Steps
1. Send a USDC transaction to `CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv`
2. Wait for 5-10 minutes (multiple poll cycles)
3. Check database: `forohtoo db list-transactions --wallet CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv`
4. Observe: Transaction not present despite being on-chain

## Impact
- **Severity**: Critical
- **Affected**: All wallets with low transaction volume (long gaps between transactions)
- **Workaround**: None currently available
- **User Impact**: Payments not being detected, workflows stuck

## Timeline
- 2025-11-17 16:27:36 UTC - Transaction sent
- 2025-11-17 16:28:21 UTC - Worker polled (45s later) - reported 0 transactions
- 2025-11-17 20:07:09 UTC - Still reporting 0 transactions (4 hours later)
- Transaction remains undetected as of investigation time

## Related Files
- `service/temporal/workflow.go` - lastSignature tracking
- `service/temporal/activities.go` - PollSolana activity
- `service/solana/client.go` - GetTransactionsSince method
- `service/solana/parser.go` - Transaction parsing

## Questions for Further Investigation
1. Does gagliardetto/solana-go set a default commitment level that we're not aware of?
2. Is there a known issue with GetSignaturesForAddress and sparse transaction histories?
3. Could there be a race condition where the transaction wasn't finalized at 16:28:21?
4. Are there any network-level filtering or caching issues with api.mainnet-beta.solana.com?
