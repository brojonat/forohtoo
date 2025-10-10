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

# Start new tmux session
tmux new-session -d -s "$SESSION_NAME" -n "main"

# Window 0: Main window with 4 panes
# Layout:
#   +-----------------+-----------------+
#   |                 |                 |
#   |   Server        |   Logs          |
#   |                 |                 |
#   +-----------------+-----------------+
#   |                 |                 |
#   |   Tests         |   Shell         |
#   |                 |                 |
#   +-----------------+-----------------+

# Create the layout
tmux split-window -h -t "$SESSION_NAME:0"
tmux split-window -v -t "$SESSION_NAME:0.0"
tmux split-window -v -t "$SESSION_NAME:0.1"

# Pane 0 (top-left): Server
tmux send-keys -t "$SESSION_NAME:0.0" "# Run server with logging" C-m
tmux send-keys -t "$SESSION_NAME:0.0" "# Example: make run | tee logs/server.log" C-m
tmux send-keys -t "$SESSION_NAME:0.0" "# or: go run ./cmd/server 2>&1 | tee logs/server.log" C-m

# Pane 1 (top-right): Logs viewer
tmux send-keys -t "$SESSION_NAME:0.1" "# Watch server logs (JSON formatted)" C-m
tmux send-keys -t "$SESSION_NAME:0.1" "# Example: tail -f logs/server.log | jq ." C-m
tmux send-keys -t "$SESSION_NAME:0.1" "# or: tail -f logs/server.log | jq 'select(.level==\"ERROR\")'" C-m

# Pane 2 (bottom-left): Tests
tmux send-keys -t "$SESSION_NAME:0.2" "# Run tests" C-m
tmux send-keys -t "$SESSION_NAME:0.2" "# Example: go test ./... -v" C-m
tmux send-keys -t "$SESSION_NAME:0.2" "# or: make test" C-m

# Pane 3 (bottom-right): Shell
tmux send-keys -t "$SESSION_NAME:0.3" "# General shell" C-m
tmux send-keys -t "$SESSION_NAME:0.3" "# Check services: docker compose ps" C-m
tmux send-keys -t "$SESSION_NAME:0.3" "# View DB: psql \$DATABASE_URL" C-m

# Select the server pane
tmux select-pane -t "$SESSION_NAME:0.0"

echo "Starting tmux session: $SESSION_NAME"
echo ""
echo "Pane layout:"
echo "  0: Server (top-left)"
echo "  1: Logs (top-right)"
echo "  2: Tests (bottom-left)"
echo "  3: Shell (bottom-right)"
echo ""
echo "Tips:"
echo "  - Switch panes: Ctrl+b then arrow key"
echo "  - Detach: Ctrl+b then d"
echo "  - Kill session: tmux kill-session -t $SESSION_NAME"
echo ""

# Attach to session
tmux attach-session -t "$SESSION_NAME"
