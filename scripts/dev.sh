#!/bin/bash
# Development tmux session for forohtoo
# Usage: ./scripts/dev.sh

set -e

SESSION_NAME="forohtoo"
LOGS_DIR="logs"

# Ensure run scripts are executable
chmod +x ./scripts/run-server.sh
chmod +x ./scripts/run-worker.sh

# Create logs directory if it doesn't exist
mkdir -p "$LOGS_DIR"

# Check if session already exists
if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    echo "Session $SESSION_NAME already exists. Attaching..."
    tmux attach-session -t "$SESSION_NAME"
    exit 0
fi

# Start new tmux session
tmux new-session -d -s "$SESSION_NAME" -n "main"

# Layout:
# +-----------+-----------+-----------+
# |  Server   |  Worker   |           |
# +-----------+-----------+   Shell   |
# | ServerLog | WorkerLog |           |
# +-----------+-----------+-----------+

# Create 3 vertical panes. This is more robust than using percentages.
tmux split-window -h
tmux split-window -h -t 0
tmux select-layout even-horizontal

# We now have 3 vertical panes: 0, 1, 2

# Split the first column for Server and Server Logs
tmux split-window -v -t 0

# Split the second column for Worker and Worker Logs
tmux split-window -v -t 1

# After splitting, the panes are numbered:
# 0: Server (top-left)
# 3: Server Logs (bottom-left)
# 1: Worker (top-middle)
# 4: Worker Logs (bottom-middle)
# 2: Shell (right)

# Pane 0: Server
tmux send-keys -t "$SESSION_NAME:main.0" "./scripts/run-server.sh" C-m

# Pane 3: Server Logs
tmux send-keys -t "$SESSION_NAME:main.3" "echo 'Waiting for server log...' && until test -f logs/server.log; do sleep 1; done; tail -f logs/server.log | jq ." C-m

# Pane 1: Worker
tmux send-keys -t "$SESSION_NAME:main.1" "./scripts/run-worker.sh" C-m

# Pane 4: Worker Logs
tmux send-keys -t "$SESSION_NAME:main.4" "echo 'Waiting for worker log...' && until test -f logs/worker.log; do sleep 1; done; tail -f logs/worker.log | jq ." C-m

# Pane 2: Shell
tmux send-keys -t "$SESSION_NAME:main.2" "# General shell" C-m

# Select the shell pane to be active
tmux select-pane -t "$SESSION_NAME:main.2"

echo "Starting tmux session: $SESSION_NAME"
echo ""
echo "Pane layout:"
echo "  - Top-left: Server"
echo "  - Bottom-left: Server Logs"
echo "  - Top-middle: Worker"
echo "  - Bottom-middle: Worker Logs"
echo "  - Right: Shell"
echo ""
echo "Tips:"
echo "  - Switch panes: Ctrl+b then arrow keys"
echo "  - Detach: Ctrl+b then d"
echo "  - Kill session: tmux kill-session -t $SESSION_NAME (or 'make stop-dev-session')"
echo ""

# Attach to session
tmux attach-session -t "$SESSION_NAME"
