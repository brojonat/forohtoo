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
# |  Server   |  Worker   |    CLI    |
# +-----------+-----------+-----------+
# |                                   |
# |           Shell                   |
# |                                   |
# +-----------------------------------+

# 1. Create a top and bottom pane. The top will be for the runners.
tmux split-window -v -l 12 # Give the top pane a fixed height of 12 lines.

# We now have:
# Pane 0: Top (12 lines)
# Pane 1: Bottom (the rest)

# 2. Select the top pane
tmux select-pane -t 0

# 3. Split the top pane into three columns for the runners
tmux split-window -h # Pane 0 becomes left, new pane 2 is right
tmux split-window -h -t 0 # Pane 0 becomes left, new pane 3 is middle

# Final pane structure:
# Top Row (left to right): Pane 0 (Server), Pane 3 (Shell), Pane 2 (CLI)
# Bottom Row: Pane 1 (Worker)

# Pane 0: Server (with air hot-reloading)
tmux send-keys -t "$SESSION_NAME:main.0" "air -c .air.server.toml" C-m

# Pane 1: Worker (with air hot-reloading)
tmux send-keys -t "$SESSION_NAME:main.1" "air -c .air.worker.toml" C-m

# Pane 2: CLI Builder (with air hot-reloading)
tmux send-keys -t "$SESSION_NAME:main.2" "air -c .air.cli.toml" C-m

# Pane 3: Shell
tmux send-keys -t "$SESSION_NAME:main.3" "# General shell" C-m
tmux send-keys -t "$SESSION_NAME:main.3" "set -a" C-m
tmux send-keys -t "$SESSION_NAME:main.3" "source .env.server.dev" C-m
tmux send-keys -t "$SESSION_NAME:main.3" "set +a" C-m

# Select the shell pane to be active
tmux select-pane -t "$SESSION_NAME:main.3"

echo "Starting tmux session: $SESSION_NAME"
echo ""
echo "Pane layout:"
echo "  - Top Row: Server | Shell | CLI Builder (hot-reloading)"
echo "  - Bottom: Worker"
echo ""
echo "Tips:"
echo "  - Switch panes: Ctrl+b then arrow keys"
echo "  - Detach: Ctrl+b then d"
echo "  - Kill session: tmux kill-session -t $SESSION_NAME (or 'make stop-dev-session')"
echo ""

# Attach to session
tmux attach-session -t "$SESSION_NAME"
