#!/bin/bash
# Development tmux session for forohtoo
# Usage: ./scripts/dev.sh

set -e

SESSION_NAME="forohtoo"
LOGS_DIR="logs"

chmod +x ./scripts/run-server.sh

mkdir -p "$LOGS_DIR"

if tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
    echo "Session $SESSION_NAME already exists. Attaching..."
    tmux attach-session -t "$SESSION_NAME"
    exit 0
fi

tmux new-session -d -s "$SESSION_NAME" -n "main"

# Layout:
# +-----------+-----------+
# |  Server   |    CLI    |
# +-----------+-----------+
# |        Shell          |
# +-----------------------+

tmux split-window -v -l 12

tmux select-pane -t 0
tmux split-window -h

tmux send-keys -t "$SESSION_NAME:main.0" "air -c .air.server.toml" C-m
tmux send-keys -t "$SESSION_NAME:main.1" "air -c .air.cli.toml" C-m

tmux send-keys -t "$SESSION_NAME:main.2" "set -a" C-m
tmux send-keys -t "$SESSION_NAME:main.2" "source .env.server.dev" C-m
tmux send-keys -t "$SESSION_NAME:main.2" "set +a" C-m

tmux select-pane -t "$SESSION_NAME:main.2"

echo "Starting tmux session: $SESSION_NAME"
echo "Pane layout: Server | CLI Builder (hot-reload top), Shell (bottom)"
echo "Detach: Ctrl+b d  |  Kill: make stop-dev-session"

tmux attach-session -t "$SESSION_NAME"
