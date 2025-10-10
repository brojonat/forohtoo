# Development Guide for Claude Code

This document provides guidance for Claude Code (and human developers) working on this project. Follow these practices to maintain code quality, consistency, and project health.

## Development Philosophy

This is a production-grade Go library and service intended for use across multiple services. Quality, reliability, and maintainability are paramount.

## Feature Development Workflow

### 1. Plan Before Coding

Before implementing any feature:

- **Understand the requirement**: Clarify the use case and acceptance criteria
- **Design the interface first**: What's the API surface? How will clients use this?
- **Consider dependencies**: What components need to interact? What can be mocked?
- **Identify edge cases**: What can go wrong? How should errors be handled?
- **Document the plan**: Write down the approach in comments or a design doc

### 2. Write Tests First (TDD)

Follow Test-Driven Development:

```go
// 1. Write the test (it will fail)
func TestWalletPoller_PollNewTransactions(t *testing.T) {
    // Arrange
    mockClient := &MockSolanaClient{}
    poller := NewWalletPoller(mockClient)

    // Act
    txns, err := poller.Poll(ctx, walletAddress)

    // Assert
    require.NoError(t, err)
    assert.Len(t, txns, 5)
}

// 2. Write minimal code to pass the test
// 3. Refactor while keeping tests green
```

**Benefits:**
- Tests document intended behavior
- Forces you to think about the interface
- Prevents untested code
- Makes refactoring safer

### 3. Server + Client Development

**Rule**: Every new server feature MUST have a corresponding client method.

**Bad:**
```go
// Server only implementation
func (s *Server) HandleAddWallet(req *AddWalletRequest) error {
    // ...
}
```

**Good:**
```go
// Server implementation
func (s *Server) HandleAddWallet(req *AddWalletRequest) error {
    // ...
}

// Client method (in client package)
func (c *Client) AddWallet(ctx context.Context, address string, interval time.Duration) error {
    req := &AddWalletRequest{Address: address, PollInterval: interval}
    // Make NATS request
    return c.request(ctx, "wallet.add", req)
}

// Test that exercises both
func TestAddWallet_EndToEnd(t *testing.T) {
    // ...
}
```

### 4. Use the Makefile

Put frequently used commands in the `Makefile` for consistency:

```makefile
.PHONY: test
test:
	go test ./... -v -race -cover

.PHONY: test-integration
test-integration:
	go test ./... -v -tags=integration

.PHONY: lint
lint:
	golangci-lint run

.PHONY: build-server
build-server:
	go build -o bin/server ./cmd/server

.PHONY: build-client
build-client:
	go build -o bin/client ./cmd/client

.PHONY: dev
dev:
	air

.PHONY: db-migrate
db-migrate:
	migrate -path migrations -database "${DATABASE_URL}" up

.PHONY: db-reset
db-reset:
	migrate -path migrations -database "${DATABASE_URL}" drop
	migrate -path migrations -database "${DATABASE_URL}" up
```

**Usage:**
```bash
make test           # Run tests
make lint           # Run linter
make dev            # Start with hot reload
make build-server   # Build server binary
```

### 5. Hot Reloading with Air

Use [Air](https://github.com/cosmtrek/air) for development:

**Install:**
```bash
go install github.com/cosmtrek/air@latest
```

**Configure** (`.air.toml`):
```toml
root = "."
tmp_dir = "tmp"

[build]
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ./cmd/server"
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "frontend"]
  include_ext = ["go", "tpl", "tmpl", "html"]
  exclude_regex = ["_test\\.go"]
```

**Run:**
```bash
make dev  # or: air
```

Air will automatically rebuild and restart the server on file changes.

### 6. Leverage tmux for Development

Use [tmux](https://github.com/tmux/tmux) to manage multiple terminal sessions efficiently. This is especially useful for running the server, client, tests, and logs simultaneously.

**Install:**
```bash
# macOS
brew install tmux

# Ubuntu/Debian
sudo apt-get install tmux

# Arch
sudo pacman -S tmux
```

**Recommended tmux Layout for Development:**

```bash
# Start a new tmux session for this project
tmux new -s solana-payment

# Split window into panes (example layout):
# ┌─────────────────────────────────────┐
# │  1. Server (air hot reload)         │
# ├─────────────────────────────────────┤
# │  2. Database logs  │  3. NATS logs  │
# ├────────────────────┼────────────────┤
# │  4. Tests/Commands │  5. Git/Editor │
# └────────────────────┴────────────────┘

# Create layout:
# Split horizontally (top/bottom)
Ctrl+b "

# Split bottom pane horizontally again
Ctrl+b "

# Split middle pane vertically
Ctrl+b :select-pane -t 1
Ctrl+b %

# Split bottom pane vertically
Ctrl+b :select-pane -t 3
Ctrl+b %
```

**Quick Setup Script:**

Create a `dev.sh` script to automate your tmux setup:

```bash
#!/bin/bash
# dev.sh - Start development environment

SESSION="solana-payment"

# Create new tmux session
tmux new-session -d -s $SESSION

# Window 0: Server
tmux rename-window -t $SESSION:0 'server'
tmux send-keys -t $SESSION:0 'make dev' C-m

# Window 1: Database & NATS
tmux new-window -t $SESSION:1 -n 'services'
tmux split-window -h -t $SESSION:1
tmux send-keys -t $SESSION:1.0 'docker-compose logs -f postgres' C-m
tmux send-keys -t $SESSION:1.1 'docker-compose logs -f nats' C-m

# Window 2: Tests
tmux new-window -t $SESSION:2 -n 'tests'
tmux send-keys -t $SESSION:2 '# Run: make test or make test-integration' C-m

# Window 3: Git & Editor
tmux new-window -t $SESSION:3 -n 'git'

# Attach to session
tmux attach-session -t $SESSION
```

**Usage:**
```bash
chmod +x dev.sh
./dev.sh
```

**Essential tmux Commands:**

```bash
# Session management
tmux new -s project-name         # Create new session
tmux attach -t project-name      # Attach to session
tmux ls                          # List sessions
tmux kill-session -t project-name # Kill session

# Window management
Ctrl+b c                         # Create new window
Ctrl+b ,                         # Rename window
Ctrl+b n                         # Next window
Ctrl+b p                         # Previous window
Ctrl+b 0-9                       # Switch to window by number
Ctrl+b w                         # List windows

# Pane management
Ctrl+b %                         # Split pane vertically
Ctrl+b "                         # Split pane horizontally
Ctrl+b arrow                     # Navigate between panes
Ctrl+b o                         # Next pane
Ctrl+b x                         # Kill pane
Ctrl+b z                         # Zoom/unzoom pane (fullscreen)
Ctrl+b spacebar                  # Toggle pane layouts

# Copy mode (scrolling)
Ctrl+b [                         # Enter copy mode
q                                # Exit copy mode
Ctrl+b ]                         # Paste buffer

# Other
Ctrl+b d                         # Detach from session
Ctrl+b ?                         # Show key bindings
Ctrl+b :                         # Command prompt
```

**Recommended `.tmux.conf`:**

Create `~/.tmux.conf` for better defaults:

```bash
# Enable mouse support
set -g mouse on

# Increase scrollback buffer
set -g history-limit 10000

# Start windows and panes at 1, not 0
set -g base-index 1
setw -g pane-base-index 1

# Better colors
set -g default-terminal "screen-256color"

# Vim-like pane navigation
bind h select-pane -L
bind j select-pane -D
bind k select-pane -U
bind l select-pane -R

# Reload config
bind r source-file ~/.tmux.conf \; display "Config reloaded!"

# Easier splits
bind | split-window -h
bind - split-window -v

# Status bar styling
set -g status-style bg=black,fg=white
set -g status-right '%Y-%m-%d %H:%M'
```

**Typical Development Workflow with tmux:**

1. **Start session**: `./dev.sh` or `tmux attach -t solana-payment`
2. **Pane 1 (Server)**: Run `make dev` for hot-reloading server
3. **Pane 2 (Database)**: Monitor database logs
4. **Pane 3 (NATS)**: Monitor NATS server
5. **Pane 4 (Tests)**: Run tests when needed: `make test`
6. **Pane 5 (Git/Commands)**: Git operations, file edits, etc.

**Benefits:**
- **No window switching**: Everything visible at once
- **Persistent sessions**: Detach/reattach without losing state
- **Scroll history**: Review logs easily in copy mode
- **Synchronized commands**: Send commands to all panes at once (if needed)

**Pro Tips:**
- Use `Ctrl+b z` to zoom a pane when you need to focus
- Use `Ctrl+b [` to scroll through logs (press `q` to exit)
- Name your windows meaningfully: `Ctrl+b ,`
- Detach with `Ctrl+b d` and your session keeps running
- Add `alias tl='tmux ls'` and `alias ta='tmux attach -t'` to your shell

## Git Workflow

### Branch Strategy

- **main**: Production-ready code, always stable
- **Feature branches**: `feature/wallet-polling`, `feature/nats-rpc`
- **Bug fixes**: `fix/jetstream-reconnect`
- **Experiments**: `experiment/timescaledb-partitioning`

**Workflow:**
```bash
# Create feature branch
git checkout -b feature/add-wallet-rpc

# Make changes, commit frequently
git add .
git commit -m "Add wallet.add RPC endpoint"

# Keep up to date with main
git fetch origin
git rebase origin/main

# When ready, merge to main
git checkout main
git merge feature/add-wallet-rpc
git push origin main

# Delete feature branch
git branch -d feature/add-wallet-rpc
```

### Commit Messages

Write comprehensive, descriptive commit messages:

**Bad:**
```
fix bug
update code
wip
```

**Good:**
```
Add NATS RPC endpoint for wallet management

Implements wallet.add, wallet.remove, and wallet.list RPC methods
using NATS request/reply pattern. Includes validation for wallet
addresses and poll interval constraints (minimum 10s).

- Add WalletManager service with NATS integration
- Implement request/reply handlers with timeout handling
- Add client methods: AddWallet, RemoveWallet, ListWallets
- Add integration tests for all RPC endpoints

Closes #42
```

**Format:**
```
[type]: Short summary (50 chars or less)

Detailed explanation of what changed and why. Wrap at 72 characters.
Include motivation, context, and any breaking changes.

- Bullet points for key changes
- Reference issues/PRs: Closes #123, Refs #456
- Note breaking changes: BREAKING: Changed API signature
```

**Types:** feat, fix, docs, refactor, test, chore, perf

## Documentation

### Always Update

When making changes, update relevant documentation:

1. **README.md**: Architecture, usage examples, getting started
2. **CHANGELOG.md**: User-facing changes (see format below)
3. **Code comments**: Complex logic, public APIs, configuration options
4. **Examples**: Add/update examples in `examples/` directory

### CHANGELOG Format

Use [Keep a Changelog](https://keepachangelog.com/) format:

```markdown
# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- NATS RPC endpoints for wallet management
- JetStream integration for transaction streaming

### Changed
- Switched from HTTP to pure NATS architecture

### Fixed
- Race condition in wallet poller shutdown

## [0.2.0] - 2025-10-05

### Added
- TimescaleDB support for long-term storage
- Transaction memo parsing

### Changed
- Database schema to support hypertables

## [0.1.0] - 2025-09-20

### Added
- Initial release
- Basic Solana wallet polling
- PostgreSQL storage
```

**When to update:**
- During feature development (add to Unreleased section)
- Before releasing (move Unreleased to new version)
- For any user-facing change

## Code Quality

### Testing Standards

**Coverage goals:**
- Unit tests: 80%+ coverage
- Integration tests for all critical paths
- E2E tests for main workflows

**Test types:**

```go
// Unit test: Fast, isolated, no external dependencies
func TestParseTransactionMemo(t *testing.T) {
    memo := `{"workflow_id": "abc123"}`
    result, err := ParseMemo(memo)
    require.NoError(t, err)
    assert.Equal(t, "abc123", result.WorkflowID)
}

// Integration test: Uses real components (DB, NATS)
// +build integration
func TestWalletPoller_WithRealDatabase(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    // ...
}

// E2E test: Full system test
func TestPaymentWorkflow_EndToEnd(t *testing.T) {
    // Start server, client, make real NATS calls
}
```

### Linting

Use `golangci-lint` with strict settings:

```bash
make lint
```

Fix all warnings before committing.

### Error Handling

**Always handle errors:**
```go
// Bad
txns, _ := client.GetTransactions(ctx)

// Good
txns, err := client.GetTransactions(ctx)
if err != nil {
    return fmt.Errorf("failed to get transactions: %w", err)
}
```

**Use structured errors:**
```go
type WalletNotFoundError struct {
    Address string
}

func (e *WalletNotFoundError) Error() string {
    return fmt.Sprintf("wallet not found: %s", e.Address)
}
```

## Project Structure

```
.
├── cmd/
│   ├── server/          # Backend service entry point
│   └── client/          # CLI client (optional)
├── internal/
│   ├── server/          # Server implementation
│   ├── poller/          # Solana polling logic
│   ├── storage/         # Database layer
│   └── nats/            # NATS integration
├── pkg/
│   └── client/          # Public client library
├── migrations/          # TimescaleDB migrations
├── examples/            # Usage examples
├── testdata/            # Test fixtures
├── Makefile
├── .air.toml           # Air configuration
├── go.mod
├── README.md
├── CHANGELOG.md
└── CLAUDE.md           # This file
```

## Performance Considerations

- **Rate Limits**: Solana RPC has rate limits; respect them in polling logic
- **Database Indexes**: Index frequently queried columns (wallet_address, block_time, workflow_id)
- **NATS Batch Processing**: Process transactions in batches when possible
- **Context Timeouts**: Always use context with timeout for external calls
- **Graceful Shutdown**: Handle SIGTERM/SIGINT properly

## Security

- **Validate Inputs**: Sanitize wallet addresses, validate memo JSON
- **Rate Limit RPCs**: Prevent abuse of wallet.add endpoint
- **Secure NATS**: Use TLS and authentication in production
- **Database Access**: Use prepared statements, never concatenate SQL
- **Secrets Management**: Never commit credentials; use environment variables

## Development Environment Setup

```bash
# Install dependencies
go mod download

# Install development tools
go install github.com/cosmtrek/air@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Start dependencies (Docker Compose)
docker-compose up -d postgres nats

# Run migrations
make db-migrate

# Start development server
make dev

# Run tests
make test
```

## Common Tasks

### Adding a New RPC Endpoint

1. **Plan**: Define request/response types, validation rules
2. **Test**: Write tests in `internal/server/rpc_test.go`
3. **Implement**: Add handler in `internal/server/rpc.go`
4. **Client**: Add method in `pkg/client/client.go`
5. **Document**: Update README with new endpoint
6. **Changelog**: Add to Unreleased section

### Adding a Database Migration

```bash
# Create migration files
migrate create -ext sql -dir migrations -seq add_transaction_index

# Edit migrations/000001_add_transaction_index.up.sql
# Edit migrations/000001_add_transaction_index.down.sql

# Test migration
make db-reset
make db-migrate
```

### Adding a New Transaction Field

1. Update database schema (migration)
2. Update struct in `internal/storage/models.go`
3. Update poller to fetch new field
4. Update JetStream message format
5. Update client parsing logic
6. Update tests
7. Document in README

## Troubleshooting

**NATS connection issues:**
- Check NATS server is running: `nats server list`
- Verify connection string in config
- Check firewall/network rules

**Database migrations failing:**
- Check DATABASE_URL environment variable
- Verify TimescaleDB extension is installed
- Check migration files for syntax errors

**Tests failing:**
- Run with verbose output: `go test -v ./...`
- Check for stale mocks or test data
- Ensure test database is clean: `make db-reset`

## Questions?

When in doubt:
- Check existing code for patterns
- Refer to Go best practices
- Ask for clarification rather than guessing
- Document decisions in commit messages
