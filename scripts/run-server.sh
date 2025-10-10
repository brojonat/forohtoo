#!/bin/bash
# Run the server with logging to file
# Usage: ./scripts/run-server.sh

set -e

# Create logs directory
mkdir -p logs

# Use test database for demo
export DATABASE_URL="postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
export SOLANA_RPC_URL="https://api.mainnet-beta.solana.com"
export SERVER_ADDR=":8080"
export LOG_LEVEL="debug"

echo "Starting server..."
echo "Database: $DATABASE_URL"
echo "Server: http://localhost$SERVER_ADDR"
echo "Logs: logs/server.log"
echo ""

# Build and run server, tee output to log file
go build -o bin/server ./cmd/server && \
    ./bin/server 2>&1 | tee logs/server.log
