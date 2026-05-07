#!/bin/bash
# Bootstrap wallet monitoring
# Registers a default set of wallets so they're streamed via the Helius webhook.
# Usage: ./scripts/bootstrap-wallets.sh

set -e

SERVER_URL="${FOROHTOO_SERVER_URL:-https://forohtoo.brojonat.com}"
CLI="${CLI:-./bin/forohtoo}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

declare -a WALLETS=(
  "CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv:mainnet"
  "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk:mainnet"
  "DWR6Xe2CSTVepw8eQxxENKwcoC7TzEYUy5oSBC8a:devnet"
)

echo "Bootstrap Wallet Monitoring"
echo "============================"
echo "Server URL: $SERVER_URL"
echo ""

if [ ! -f "$CLI" ]; then
  echo -e "${RED}Error: CLI not found at $CLI${NC}"
  echo "Run 'make build-cli' first"
  exit 1
fi

for wallet_spec in "${WALLETS[@]}"; do
  IFS=':' read -r address network <<< "$wallet_spec"

  echo -e "${YELLOW}Registering wallet:${NC} $address ($network)"

  if $CLI wallet add "$address" \
    --server "$SERVER_URL" \
    --network "$network" 2>&1; then
    echo -e "${GREEN}✓ Registered/Updated successfully${NC}"
  else
    echo -e "${RED}✗ Failed to register${NC}"
    exit 1
  fi
  echo ""
done

echo "============================"
echo -e "${GREEN}Bootstrap complete!${NC}"
echo ""
echo "Registered wallets:"
$CLI wallet list --server "$SERVER_URL"
