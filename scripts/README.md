# Reprocessing Scripts

## reprocess_null_from_address.py

Reprocesses transactions that have `NULL` for `from_address` due to the Transfer instruction parser bug.

### Prerequisites

- [uv](https://docs.astral.sh/uv/) installed
- Access to the PostgreSQL database
- Access to Solana RPC

### Usage

**From Kubernetes worker pod:**
```bash
# Copy script to pod
kubectl cp scripts/reprocess_null_from_address.py \
  $(kubectl get pods -l app=forohtoo-worker -o jsonpath='{.items[0].metadata.name}'):/tmp/

# Install uv in pod (if not already installed)
kubectl exec $(kubectl get pods -l app=forohtoo-worker -o jsonpath='{.items[0].metadata.name}') -- \
  sh -c 'curl -LsSf https://astral.sh/uv/install.sh | sh'

# Run the script (DATABASE_URL is already set in pod environment)
kubectl exec $(kubectl get pods -l app=forohtoo-worker -o jsonpath='{.items[0].metadata.name}') -- \
  /root/.local/bin/uv run /tmp/reprocess_null_from_address.py \
  --wallet CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv \
  --network mainnet
```

**From local machine:**
```bash
# Set DATABASE_URL environment variable
export DATABASE_URL="postgresql://user:pass@host:5432/db?sslmode=require"

# Run the script
./scripts/reprocess_null_from_address.py \
  --wallet CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv \
  --network mainnet
```

### Options

- `--wallet`: Wallet address to process (required)
- `--network`: Network (mainnet or devnet, required)
- `--limit`: Maximum number of transactions to process (default: 1000)
- `--database-url`: PostgreSQL connection string (or use DATABASE_URL env var)
- `--rpc-url`: Solana RPC URL (or use SOLANA_MAINNET_RPC_URL/SOLANA_DEVNET_RPC_URL env var)

### What it does

1. Fetches all transactions with `from_address IS NULL` for the given wallet and network
2. For each transaction:
   - Fetches the full transaction from Solana RPC
   - Parses the token transfer instructions
   - Extracts the authority (signer) as the `from_address`
   - Updates the database
3. Respects rate limits (~1.5 RPS for public RPC)

### Example Output

```
🔍 Reprocessing transactions for wallet: CZ2BWqd96adrqdpdRJYHZgeMKK36UB3oMFBAqwk3e4wv
   Network: mainnet
   RPC: https://api.mainnet-beta.solana.com
   Database: 5.78.109.232:5432/forohtoo

📊 Fetching transactions with NULL from_address...
📝 Found 110 transactions with NULL from_address

[1/110] Processing 5y9cmsBAZ7BAjtm7hYxi... (2025-11-17 16:27:36)
  ✅ Updated: from_address = Fp84XY5Fjbf6t8JgYfktpP5zwxooGw1QeK8fBYzNiaW4
[2/110] Processing 53A5KgqyfuM6gH7zWMjb... (2025-11-17 15:42:11)
  ✅ Updated: from_address = Fp84XY5Fjbf6t8JgYfktpP5zwxooGw1QeK8fBYzNiaW4
...

============================================================
📊 Summary:
   Total processed: 110
   ✅ Updated: 108
   ⚠️  Not found: 2
   ❌ Failed: 0
============================================================
```
