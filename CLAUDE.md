# Development Guide for Claude Code When Writing Go

This document provides guidance for Claude Code (and human developers) working on this project. Follow these practices to maintain code quality, consistency, and project health.

For concrete code examples and patterns, see **[PATTERNS.md](./PATTERNS.md)**.

## Development Philosophy

This is a production-grade Go library and service. Quality, reliability, and maintainability are paramount.

## Feature Development Workflow

### 1. Plan Before Coding

Before implementing any feature:

- **Understand the requirement**: Clarify the use case and acceptance criteria
- **Design the interface first**: What's the API surface? How will clients use this?
- **Consider dependencies**: What components need to interact? What can be mocked?
- **Identify edge cases**: What can go wrong? How should errors be handled?
- **Document the plan**: Write down the approach in comments or a design doc

### 2. Write Tests First (TDD)

Follow Test-Driven Development (within reason). See [PATTERNS.md](./PATTERNS.md#test-driven-development) for examples.

**Benefits:**
- Tests document intended behavior
- Forces you to think about the interface
- Prevents untested code
- Makes refactoring safer

### 3. Server + Client Development

**Rule**: Every new server feature should have a corresponding client method, ideally one that's accessible via a CLI (sub)command.

See [PATTERNS.md](./PATTERNS.md#server--client-development) for examples.

### 4. Use the Makefile

Put frequently used commands in the `Makefile` for consistency. See [PATTERNS.md](./PATTERNS.md#makefile-template) for a template.

### 5. Hot Reloading with Air

Use [Air](https://github.com/cosmtrek/air) for development hot reloading. See [PATTERNS.md](./PATTERNS.md#air-configuration) for configuration.

### 6. Leverage tmux for Development

Use [tmux](https://github.com/tmux/tmux) to manage multiple terminal sessions efficiently. Run server, tests, and logs simultaneously in split panes.

## Git Workflow

### Branch Strategy

- **main**: Production-ready code, always stable
- **Feature branches**: `feature/user-auth`, `feature/api-endpoints`
- **Bug fixes**: `fix/connection-retry`
- **Experiments**: `experiment/new-approach`

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
Add user authentication with JWT

Implements JWT-based authentication with refresh tokens.
Includes validation for token expiry and signature verification.

- Add authentication middleware
- Implement token generation and validation
- Add integration tests for auth flow
- Add client methods: Login, Refresh, Logout

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
- New feature description

### Changed
- Changed behavior description

### Fixed
- Bug fix description

## [0.2.0] - 2025-10-05

### Added
- Feature added in this release
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

**Test types:** See [PATTERNS.md](./PATTERNS.md#test-types) for examples.

### Linting

Use `golangci-lint` with strict settings:

```bash
make lint
```

Fix all warnings before committing.

### Error Handling

Always handle errors. Never use blank identifier `_` for errors. Use `fmt.Errorf` with `%w` for wrapping. See [PATTERNS.md](./PATTERNS.md#error-handling) for examples.

## Security

- **Secrets Management**: Never commit credentials; use environment variables and _NEVER_ commit a .env file to version control!

## Design Philosophy

This project embraces the Unix philosophy and the Zen of Python to create tools that are simple, composable, and predictable.

### Core Principles

**Do One Thing Extremely Well**

Each component has a single, well-defined responsibility. Resist the temptation to add unrelated features. If you need additional functionality, write a separate tool that composes with existing components.

**Write Programs That Compose**

Design components to work together through standard interfaces. Outputs should be inputs for other tools. This enables:

- Analytics tools that aggregate data
- Alerting systems that filter by conditions
- Custom workflows that react to specific patterns

**Simple Is Better Than Complex**

Avoid overengineering. Prefer:

- Plain JSON over custom binary protocols
- Simple message passing over complex routing logic
- Standard SQL queries over ORM magic
- Environment variables over elaborate config DSLs

Complex solutions should be justified by complex problems, not anticipated future requirements.

**Make Your Dependencies Explicit**

Following [go-kit](https://gokit.io/) philosophy, all dependencies should be explicit and passed as parameters. Never hide dependencies in global state, singletons, or package-level variables.

See [PATTERNS.md](./PATTERNS.md#explicit-dependencies) for examples.

**Benefits:**

- **Testability**: Easy to mock dependencies in tests
- **Clarity**: You can see exactly what a component needs
- **Flexibility**: Swap implementations (e.g., Postgres → SQLite for tests)
- **No hidden coupling**: Dependencies are visible in type signatures
- **Lifecycle management**: Clear ownership of resources

**Apply this everywhere:**

- Constructors take dependencies as parameters
- Use interfaces for external dependencies (DB, message brokers, external APIs)
- Avoid `init()` functions that set up global state
- Avoid package-level variables for stateful dependencies
- Pass `context.Context` as the first parameter to all functions

Reading the struct definition tells you everything the component depends on. No surprises.

**Avoid Frameworks, Embrace the Standard Library**

Frameworks often make the above goals harder by hiding complexity and coupling your code to their abstractions. Instead, write functions that return `http.Handler` and use the standard library router.

**Handler Functions Pattern:**

Following [Mat Ryer's](https://pace.dev/blog/2018/05/09/how-I-write-http-services-after-eight-years.html) approach, write functions that return `http.Handler`. See [PATTERNS.md](./PATTERNS.md#http-handler-pattern) for examples.

**Benefits:**

- Dependencies are explicit (passed as parameters)
- Easy to test (just call the function and test the handler)
- No framework magic or hidden behavior
- Handler has everything it needs in its closure

**Middleware Pattern with adaptHandler:**

Use an `adaptHandler` function to compose middleware. It iterates middleware in reverse order so the first supplied is called first. See [PATTERNS.md](./PATTERNS.md#middleware-pattern) for complete examples including:

- Logging middleware
- JWT authentication
- Request ID tracking
- Prometheus metrics
- Response helpers
- Health check handlers
- Route wiring

No framework, no magic, just plain Go. This keeps the code simple, explicit, and easy to reason about.

**Go Tooling Preferences**

**CLI with urfave/cli:**

Use the [urfave/cli](https://github.com/urfave/cli) library for building command-line interfaces. It provides a clean, composable API for flags, commands, and subcommands. See [PATTERNS.md](./PATTERNS.md#cli-with-urfavecli) for examples.

**Benefits:**
- Clean flag/command API
- Automatic environment variable binding
- Built-in help generation
- Subcommands for different services
- Consistent CLI experience across all tools

**SQL Generation with sqlc:**

Use [sqlc](https://sqlc.dev/) to generate type-safe Go code from SQL. Write SQL, get Go. See [PATTERNS.md](./PATTERNS.md#sqlc-configuration) for configuration and usage examples.

**Benefits:**
- **Type Safety**: Compile-time SQL validation
- **No ORM Magic**: You write SQL, sqlc generates Go
- **Performance**: Direct SQL execution, no reflection
- **Explicit**: Generated code is readable and debuggable
- **Maintainable**: SQL is version controlled alongside code
- **PostgreSQL/TimescaleDB**: Full support for advanced features

**Why sqlc over ORMs:**
- ORMs hide complexity and make optimizations harder
- SQL is explicit and portable
- Generated code is just functions - no framework lock-in
- Easy to optimize queries without fighting the abstraction
- You already know SQL - no need to learn ORM DSL

Always regenerate sqlc code after schema changes and commit the generated code to version control.

**Be Quiet by Default**

Programs should only output information when there's something unexpected to report. Normal operation should be silent. Only output errors or significant events.

Use structured logging (JSON) for debugging and metrics, but send it to stderr or a log file, not stdout. Reserve stdout for actionable output.

**Structured Logging with slog**

Use Go's built-in `slog` package for all logging. Default to DEBUG level for most log messages so verbosity can be easily controlled via environment variables.

See [PATTERNS.md](./PATTERNS.md#structured-logging-with-slog) for complete examples including:
- Logger setup with configurable levels
- Logging at appropriate levels (DEBUG, INFO, WARN, ERROR)
- Environment variable configuration
- Development logging to files with `tee`
- Log querying with `jq`

**When You Write to Stdout, Use JSON**

If a program produces output, make it machine-readable. Use JSON by default, optionally provide a `--format` flag for human-friendly output.

**Exceptions:**
- Interactive CLI tools can use human-friendly formatting (but offer `--json` flag)
- Error messages to stderr can be plain text
- Log files can use structured formats (JSON, logfmt)

### Practical Applications

**Backend Service:**
- Runs silently in production
- Logs errors/warnings to stderr as JSON
- Exposes metrics via Prometheus endpoint (not stdout)
- No verbose logging for normal operation

**Client Library:**
- Returns errors, doesn't print them
- No "connecting..." messages
- Caller decides what to log

**CLI Tools:**
- Default to JSON output on stdout
- Provide `--format=table|json|csv` flag for human use
- Errors go to stderr
- Exit codes indicate success/failure (0 = success, non-zero = error)

### Why This Matters

These principles make the system:

- **Debuggable**: JSON logs are easily parsed and analyzed
- **Composable**: Outputs become inputs for other tools
- **Scriptable**: Predictable behavior enables automation
- **Maintainable**: Simple components are easier to understand and modify
- **Resilient**: Single-purpose tools fail independently

When in doubt, ask: "Does this add essential value, or does it just add complexity?"

## Questions?

When in doubt:

- Check existing code for patterns
- Refer to [PATTERNS.md](./PATTERNS.md) for examples
- Refer to Go best practices
- Ask for clarification rather than guessing
- Document decisions in commit messages
