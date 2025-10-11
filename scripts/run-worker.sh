#!/bin/bash
# Run the worker with logging to file
# Usage: ./scripts/run-worker.sh

set -e

# Create logs directory
mkdir -p logs

set -a
source .env.worker.dev
set +a

echo "Starting worker..."
echo "Database: $DATABASE_URL"
echo "Temporal: $TEMPORAL_HOST"
echo "Logs: logs/worker.log"
echo ""

# Build and run worker, tee output to log file
go build -o bin/worker ./cmd/worker && \
    ./bin/worker 2>&1 | tee logs/worker.log
