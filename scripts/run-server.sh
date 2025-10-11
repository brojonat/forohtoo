#!/bin/bash
# Run the server with logging to file
# Usage: ./scripts/run-server.sh

set -e

# Create logs directory
mkdir -p logs

set -a
source .env.server.dev
set +a

echo "Starting server..."
echo "Database: $DATABASE_URL"
echo "Server: http://localhost$SERVER_ADDR"
echo "Logs: logs/server.log"
echo ""

# Build and run server, tee output to log file
go build -o bin/server ./cmd/server && \
    ./bin/server 2>&1 | tee logs/server.log
