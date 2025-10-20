#!/bin/bash
# Bootstrap wallet monitoring
# Registers a default set of wallets for monitoring
# Usage: ./scripts/bootstrap-wallets.sh

set -e  # Exit on error

# Configuration
SERVER_URL="${FOROHTOO_SERVER_URL:-https://forohtoo.brojonat.com}"
CLI="${CLI:-./bin/forohtoo}"
POLL_INTERVAL="30s"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Wallets to bootstrap
declare -a WALLETS=(
  "CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv:mainnet"
  "9roL4UsoNzxgypchxXAkti7opEoLLYXz8DihEQuLQwqk:mainnet"
  "DWR6Xe2CSTVepw8eQxxENKwcoC7TzEYUy5oSBC8a:devnet"
)

echo "Bootstrap Wallet Monitoring"
echo "============================"
echo "Server URL: $SERVER_URL"
echo "Poll Interval: $POLL_INTERVAL"
echo ""

# Check if CLI exists
if [ ! -f "$CLI" ]; then
  echo -e "${RED}Error: CLI not found at $CLI${NC}"
  echo "Run 'make build-cli' first"
  exit 1
fi

# Register each wallet (upserts automatically)
for wallet_spec in "${WALLETS[@]}"; do
  IFS=':' read -r address network <<< "$wallet_spec"

  echo -e "${YELLOW}Registering wallet:${NC} $address ($network)"

  if $CLI wallet add "$address" \
    --server "$SERVER_URL" \
    --network "$network" \
    --poll-interval "$POLL_INTERVAL" 2>&1; then
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

# Show registered wallets
echo "Registered wallets:"
$CLI wallet list --server "$SERVER_URL" | tail -n +2 || \
  $CLI --server-url "$SERVER_URL" wallet list --json | jq -r '.[] | "\(.address) (\(.network)) - \(.poll_interval)"'

echo ""
echo "You can view wallet details with:"
echo "  $CLI wallet get <ADDRESS> --server $SERVER_URL"
