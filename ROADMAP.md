# Roadmap

This document tracks planned improvements, features, and technical debt for the forohtoo project.

## High Priority

No high priority items at this time. See Medium Priority section below.

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

### Fix Broken Test Suite
**Completed**: 2026-01-12
**Commit**: `b564053`

Fixed compilation errors and test failures in the test suite. All tests now compile successfully and unit tests pass.

**Issues fixed:**
1. **client/wallet_test.go** - SSE streaming tests
   - Fixed endpoint path assertions (correct SSE streaming endpoint)
   - Added proper SSE event format (`event: transaction` prefix)
   - Fixed connection keep-alive to prevent premature closure
   - Fixed wrapped error assertions using `ErrorIs`

2. **service/temporal/workflow_payment_integration_test.go** - Already passing (import issues were resolved)

3. **service/server/integration_test.go** - Already passing (function signature issues were resolved)

**Impact**: All unit tests pass. Integration tests fail as expected without running services (database, Temporal). Test suite is healthy and CI-ready.

---

### Fix 429 Rate Limit Transaction Handling
**Completed**: 2026-01-12
**Commit**: `d4723e5`

Fixed critical bug where transactions that failed to fetch details (due to 429 rate limits or other errors) were stored in the database with incorrect `amount=0` values. Now these transactions are skipped and will be redetected on the next poll cycle, ensuring only complete and accurate transactions are stored.

**Implementation**: Removed fallback to `signatureToDomain()` for failed transactions. Added metrics tracking (`RecordTransactionsSkipped`) and enhanced logging to show signatures_received vs transactions_processed.

**Impact**: No more incorrect $0.00 amounts in user transaction history. Transactions retry naturally on next poll (~30s).

---

### Multi-Endpoint RPC Support
**Completed**: 2025-12-30 (deployed)
**Merged**: 2026-01-12 (PR #3, commit `b452b2d`)

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
