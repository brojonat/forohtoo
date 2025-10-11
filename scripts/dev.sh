#!/bin/bash
# Development tmux session for forohtoo
# Usage: ./scripts/dev.sh

set -e

SESSION_NAME="forohtoo"
LOGS_DIR="logs"

# Create logs directory if it doesn't exist
mkdir -p "$LOGS_DIR"

# Check if session already exists
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    echo "Session $SESSION_NAME already exists. Attaching..."
    tmux attach-session -t "$SESSION_NAME"
    exit 0
fi

# Start new tmux session and name the first window "Server"
tmux new-session -d -s "$SESSION_NAME" -n "Server"
tmux send-keys -t "$SESSION_NAME:Server.0" "./scripts/run-server.sh" C-m

# Create Worker window
tmux new-window -n "Worker" -t "$SESSION_NAME"
tmux send-keys -t "$SESSION_NAME:Worker.0" "./scripts/run-worker.sh" C-m

# Create Server Logs window
tmux new-window -n "Server Logs" -t "$SESSION_NAME"
tmux send-keys -t "$SESSION_NAME:Server Logs.0" "echo 'Waiting for server log...' && until test -f logs/server.log; do sleep 1; done; tail -f logs/server.log | jq ." C-m

# Create Worker Logs window
tmux new-window -n "Worker Logs" -t "$SESSION_NAME"
tmux send-keys -t "$SESSION_NAME:Worker Logs.0" "echo 'Waiting for worker log...' && until test -f logs/worker.log; do sleep 1; done; tail -f logs/worker.log | jq ." C-m

# Create Shell window
tmux new-window -n "Shell" -t "$SESSION_NAME"
tmux send-keys -t "$SESSION_NAME:Shell.0" "# General shell" C-m

# Select the Server window to be active
tmux select-window -t "$SESSION_NAME:Server"

echo "Starting tmux session: $SESSION_NAME"
echo ""
echo "Windows created:"
echo "  - Server"
echo "  - Worker"
echo "  - Server Logs"
echo "  - Worker Logs"
echo "  - Shell"
echo ""
echo "Tips:"
echo "  - Switch windows: Ctrl+b then window number (e.g., Ctrl+b 0)"
echo "  - Detach: Ctrl+b then d"
echo "  - Kill session: tmux kill-session -t $SESSION_NAME (or 'make stop-dev-session')"
echo ""

# Attach to session
tmux attach-session -t "$SESSION_NAME"
