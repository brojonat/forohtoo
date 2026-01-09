# Roadmap

This document tracks planned improvements, features, and technical debt for the forohtoo project.

## High Priority

### 1. Fix 429 Rate Limit Transaction Handling

**Status**: 🔴 Investigation needed

**Problem**: When we encounter 429 rate limit errors while fetching transaction details, we may be storing transactions with incorrect `amount=0.0` values. This is worse than not storing the transaction at all, because:
- Users see incorrect $0.00 amounts in their transaction history
- The transaction is marked as "processed" so we won't retry fetching it on subsequent polls
- No way to distinguish between actual $0 transactions and failed parses

**Desired behavior**: If we can't parse transaction details due to rate limits:
- DO NOT store the transaction in the database
- Allow the transaction to be redetected on the next poll cycle
- Once rate limits clear, process it normally with correct amount

**Root cause identified**:
- ✅ Location: `service/solana/client.go:256-268` and `client.go:272-286`
- ✅ When `GetTransaction()` fails (429 or other error), code falls back to `signatureToDomain(sig)`
- ✅ `signatureToDomain()` creates metadata-only transaction with `Amount = 0` (uint64 default)
- ✅ Comment in code: "Note: Amount, TokenMint, and Memo are not available in the signature list"
- ✅ These zero-amount transactions are then stored in database
- ✅ Affects both SOL and SPL token transfers

**Investigation tasks**:
- [x] Review current transaction parsing logic in `service/solana/client.go`
- [x] Identify where 429 errors occur during transaction processing
- [x] Trace code path from signature detection → detail fetching → database storage
- [x] Check if we have error handling that defaults to 0.0 amounts
- [ ] Review production logs for patterns of 0 amount transactions correlated with 429 errors
- [ ] Query database for count of transactions with amount=0 vs non-zero
- [ ] Correlate zero-amount transactions with worker logs showing 429 errors

**Proposed solution options**:

**Option A: Skip transactions with failed detail fetch** (Recommended)
- Modify `client.go:256-268` to NOT append transaction when `GetTransaction()` fails
- Remove the fallback to `signatureToDomain()` for detail fetch failures
- Transaction remains unprocessed and will be redetected on next poll
- Add counter metric: `transactions_skipped_detail_fetch_failed`
- Log at DEBUG level (not WARN) since this is expected retry behavior

**Option B: Mark transactions as "pending parse"**
- Add a `parse_status` field to transactions table (`parsed`, `pending`, `failed`)
- Store metadata-only transaction with `parse_status=pending` and `amount=NULL`
- Background job or next poll retries parsing pending transactions
- More complex but provides transaction history continuity

**Option C: Exponential backoff retry in same poll cycle**
- Retry detail fetch with exponential backoff (already doing this, but still failing)
- If still failing after retries, use Option A (skip transaction)

**Recommended approach**: Option A - simplest, safest, and aligns with polling model
- Signatures don't disappear - they'll be fetched again on next poll
- Once rate limits clear, transaction processes normally with correct amount
- No risk of storing incorrect data
- Minimal code changes

**Implementation files**:
- `service/solana/client.go:256-268` - GetTransaction failure handling
- `service/solana/client.go:272-286` - Parse transaction failure handling
- `service/solana/parser.go:45-65` - `signatureToDomain()` function (creates 0-amount txns)
- `service/solana/types.go:14` - `Amount uint64` field (defaults to 0)

**References**:
- Production logs show ongoing 429 errors (see worker logs: "failed to get transaction details after retries")
- User feedback indicates $0 amounts appearing in transaction history
- Related to multi-endpoint RPC work (partially mitigated by load distribution)

**Priority justification**: This directly affects data accuracy and user trust. Incorrect amounts are worse than missing transactions.

---

### 2. Fix Broken Test Suite

**Status**: 🔴 Blocking CI

**Problem**: Several tests are failing due to compilation errors and signature mismatches unrelated to recent feature work:

**Failing tests**:
1. `service/temporal/workflow_payment_integration_test.go`
   - Duplicate import of `"go.temporal.io/sdk/client"`
   - Undefined `client.Dial` and `client.Options` (import/package issue)
   - Line 15: duplicate `client` declaration

2. `service/server/integration_test.go`
   - `server.New()` signature mismatch (lines 54, 63, 211)
   - Expecting 7 parameters but tests provide 8
   - Likely due to refactoring of `server.New` constructor

3. `client/wallet_test.go`
   - SSE streaming endpoint path mismatch
   - Unexpected SSE stream closure in `TestClient_Await_MatchingTransaction`
   - Timeout test expecting wrong error type

**Tasks**:
- [ ] Fix import issues in `workflow_payment_integration_test.go`
- [ ] Update `server.New()` call signatures in integration tests
- [ ] Fix SSE endpoint path in client tests
- [ ] Run full test suite with race detector: `make test`
- [ ] Verify integration tests (requires running services): `make test-integration`
- [ ] Update CI configuration if needed

**Priority justification**: Broken tests reduce confidence in deployments and make it harder to catch regressions.

---

## Medium Priority

### 3. Enhanced Rate Limit Handling

**Status**: 🟡 Future work

**Description**: While multi-endpoint RPC support helps distribute load, we still see 429 errors under heavy load. Consider additional strategies:

**Potential improvements**:
- [ ] Implement exponential backoff for 429 errors (currently using Temporal's default retry)
- [ ] Add endpoint rotation on rate limit errors (try different endpoint if one hits limits)
- [ ] Circuit breaker pattern: temporarily disable endpoints with persistent 429s
- [ ] Rate limit detection and dynamic polling interval adjustment
- [ ] Add configurable per-endpoint rate limits in code
- [ ] Implement request queuing/throttling before hitting external rate limits

**Metrics to add**:
- Per-endpoint 429 error counts
- Per-endpoint success/failure rates
- Average response time per endpoint
- Track which endpoints are hitting limits most frequently

**Considerations**:
- May need to make endpoint selection per-request instead of per-worker-startup
- Trade-off between complexity and benefit
- Could start with simpler retry-with-different-endpoint approach

---

### 4. Transaction Reprocessing Tools

**Status**: 🟡 Partial tooling exists

**Description**: Tools to identify and reprocess transactions that may have been stored incorrectly.

**Existing work**:
- Script to find transactions with NULL from_address: `scripts/reprocess-null-from-address.sql`
- Recent commits show work in this area (b19cde6, 5391088)

**Additional tooling needed**:
- [ ] Query to find transactions with suspicious 0.0 amounts
- [ ] Reprocessing script that:
  - Identifies likely failed transactions (0.0 amounts + 429 error correlation)
  - Deletes from database
  - Allows natural redetection on next poll cycle
- [ ] Admin CLI command: `forohtoo admin reprocess --wallet ADDRESS --since TIMESTAMP`
- [ ] Add `reprocessed` flag to transactions table to track reprocessing history

---

### 5. Monitoring and Alerting Improvements

**Status**: 🟡 Basic metrics exist

**Description**: Enhance observability for production operations.

**Metrics to add**:
- [ ] Transaction parsing success/failure rates
- [ ] Per-endpoint RPC call distribution (verify random selection is working)
- [ ] Rate limit error rates (by endpoint and overall)
- [ ] Transaction storage error rates
- [ ] Polling lag metrics (time between transaction occurrence and detection)
- [ ] Average transaction amounts (to detect 0.0 anomalies)

**Alerting**:
- [ ] Alert on elevated 429 error rates
- [ ] Alert on high percentage of 0.0 amount transactions
- [ ] Alert on worker crash loops
- [ ] Alert on database connection issues

---

## Low Priority / Future Considerations

### 6. Advanced Multi-Endpoint Strategies

**Status**: 🟢 Current implementation sufficient

**Description**: The current random selection strategy works well for basic load distribution. Future enhancements could include:

- [ ] Smart endpoint selection based on historical performance
- [ ] Per-request endpoint rotation (instead of per-worker selection)
- [ ] Weighted random selection (prefer faster/more reliable endpoints)
- [ ] Health checking of endpoints before selection
- [ ] Automatic endpoint removal if consistently failing
- [ ] Support for endpoint-specific configuration (rate limits, timeouts, priorities)

**Note**: Only pursue if current strategy proves insufficient. Don't over-engineer.

---

### 7. Cost Optimization

**Status**: 🟢 Already achieved ~75% cost reduction

**Description**: Continue optimizing RPC costs while maintaining reliability.

**Ideas**:
- [ ] Monitor per-endpoint usage and costs
- [ ] Adjust free/paid endpoint ratios based on actual usage patterns
- [ ] Consider additional free endpoints (Triton, GenesysGo, Serum)
- [ ] Implement caching for frequently accessed transaction data
- [ ] Batch RPC calls where possible
- [ ] Use WebSocket subscriptions instead of polling (major architecture change)

---

### 8. Testing Improvements

**Status**: 🟡 Good coverage, could be better

**Description**: Enhance test coverage and reliability.

**Tasks**:
- [ ] Add integration tests for multi-endpoint RPC selection
- [ ] Add tests for 429 error handling
- [ ] Mock RPC responses for more deterministic tests
- [ ] Add end-to-end tests for full transaction lifecycle
- [ ] Add load tests to verify rate limit handling
- [ ] Set up continuous integration with all services (currently skipping DB/Temporal tests)

---

## Completed ✅

### Multi-Endpoint RPC Support
**Completed**: 2025-12-30 (deployed)
**Merged**: Pending (feature branch ready)

Workers now distribute load across multiple Solana RPC endpoints, reducing costs by ~75% and improving resilience against rate limits.

---

## Notes

**Prioritization criteria**:
1. **Data correctness** - Incorrect amounts are worse than missing data
2. **User impact** - Issues affecting end-user experience
3. **Operational stability** - System reliability and observability
4. **Cost efficiency** - Reducing operational costs
5. **Developer experience** - Code quality, testing, documentation

**Contributing**:
When working on roadmap items:
- Update the status as you progress (🔴 Not started → 🟡 In progress → 🟢 Complete)
- Check off tasks as completed
- Move completed items to the "Completed" section with date
- Add new items as they're identified
- Reference commit SHAs and PRs in the Completed section

**Review cadence**:
- Review roadmap monthly
- Reprioritize based on production issues and user feedback
- Archive completed items older than 6 months
