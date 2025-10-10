#!/bin/bash
# Test the wallet API
# Usage: ./scripts/test-api.sh

BASE_URL="http://localhost:8080"

echo "Testing Wallet Management API"
echo "=============================="
echo ""

# Test 1: Health check
echo "1. Health check..."
curl -s "$BASE_URL/health"
echo -e "\n"

# Test 2: Register a wallet
echo "2. Register wallet (wallet123)..."
curl -s -X POST "$BASE_URL/api/v1/wallets" \
  -H "Content-Type: application/json" \
  -d '{"address":"wallet123","poll_interval":"30s"}' | jq .
echo ""

# Test 3: Get wallet
echo "3. Get wallet (wallet123)..."
curl -s "$BASE_URL/api/v1/wallets/wallet123" | jq .
echo ""

# Test 4: Register another wallet
echo "4. Register wallet (wallet456)..."
curl -s -X POST "$BASE_URL/api/v1/wallets" \
  -H "Content-Type: application/json" \
  -d '{"address":"wallet456","poll_interval":"1m"}' | jq .
echo ""

# Test 5: List all wallets
echo "5. List all wallets..."
curl -s "$BASE_URL/api/v1/wallets" | jq .
echo ""

# Test 6: Try to register duplicate
echo "6. Try to register duplicate (should fail)..."
curl -s -X POST "$BASE_URL/api/v1/wallets" \
  -H "Content-Type: application/json" \
  -d '{"address":"wallet123","poll_interval":"30s"}' | jq .
echo ""

# Test 7: Get non-existent wallet
echo "7. Get non-existent wallet (should fail)..."
curl -s "$BASE_URL/api/v1/wallets/nonexistent" | jq .
echo ""

# Test 8: Unregister wallet
echo "8. Unregister wallet (wallet123)..."
curl -s -X DELETE "$BASE_URL/api/v1/wallets/wallet123"
echo -e "\n"

# Test 9: Verify unregistration
echo "9. Verify wallet123 is gone..."
curl -s "$BASE_URL/api/v1/wallets/wallet123" | jq .
echo ""

# Test 10: List wallets (should only show wallet456)
echo "10. List wallets (should only show wallet456)..."
curl -s "$BASE_URL/api/v1/wallets" | jq .
echo ""

echo "Tests complete!"
