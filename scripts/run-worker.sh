#!/bin/bash
# Run the worker with logging to file
# Usage: ./scripts/run-worker.sh

set -e

# Create logs directory
mkdir -p logs

# Use test database for demo
export DATABASE_URL="postgres://postgres:postgres@localhost:5433/forohtoo_test?sslmode=disable"
export TEMPORAL_ADDRESS="localhost:7233"
export LOG_LEVEL="debug"

echo "Starting worker..."
echo "Database: $DATABASE_URL"
echo "Temporal: $TEMPORAL_ADDRESS"
echo "Logs: logs/worker.log"
echo ""

# Build and run worker, tee output to log file
go build -o bin/worker ./cmd/worker && \
    ./bin/worker 2>&1 | tee logs/worker.log
