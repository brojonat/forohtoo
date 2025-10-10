.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: test
test: ## Run all tests
	go test ./... -v -race -cover

.PHONY: test-integration
test-integration: ## Run integration tests
	go test ./... -v -tags=integration

.PHONY: lint
lint: ## Run linter
	golangci-lint run

.PHONY: build-server
build-server: ## Build server binary
	go build -o bin/server ./cmd/server

.PHONY: build-cli
build-cli: ## Build CLI binary
	go build -o bin/forohtoo ./cmd/forohtoo

.PHONY: build
build: build-server build-cli ## Build all binaries

.PHONY: run
run: build-server ## Build and run the server
	./bin/server

.PHONY: run-dev
run-dev: ## Run server with hot reload using air
	air

.PHONY: dev
dev: ## Start tmux development session
	./scripts/dev.sh

.PHONY: sqlc-generate
sqlc-generate: ## Generate Go code from SQL queries
	sqlc generate

.PHONY: sqlc-verify
sqlc-verify: ## Verify SQL queries
	sqlc verify

.PHONY: db-migrate-up
db-migrate-up: ## Run database migrations
	migrate -path service/db/migrations -database "${DATABASE_URL}" up

.PHONY: db-migrate-down
db-migrate-down: ## Rollback database migrations
	migrate -path service/db/migrations -database "${DATABASE_URL}" down

.PHONY: db-migrate-create
db-migrate-create: ## Create a new migration (usage: make db-migrate-create NAME=migration_name)
	migrate create -ext sql -dir service/db/migrations -seq $(NAME)

.PHONY: db-reset
db-reset: ## Reset database (drop and recreate)
	migrate -path service/db/migrations -database "${DATABASE_URL}" drop -f
	migrate -path service/db/migrations -database "${DATABASE_URL}" up

.PHONY: db-test-migrate-up
db-test-migrate-up: ## Run migrations on test database
	migrate -path service/db/migrations -database "${TEST_DATABASE_URL}" up

.PHONY: db-test-migrate-down
db-test-migrate-down: ## Rollback migrations on test database
	migrate -path service/db/migrations -database "${TEST_DATABASE_URL}" down

.PHONY: db-test-reset
db-test-reset: ## Reset test database
	migrate -path service/db/migrations -database "${TEST_DATABASE_URL}" drop -f
	migrate -path service/db/migrations -database "${TEST_DATABASE_URL}" up

.PHONY: test-db
test-db: db-test-reset test ## Reset test database and run tests

.PHONY: docker-up
docker-up: ## Start Docker services (Postgres, NATS)
	docker-compose up -d

.PHONY: docker-down
docker-down: ## Stop Docker services
	docker-compose down

.PHONY: docker-logs
docker-logs: ## Follow Docker logs
	docker-compose logs -f

.PHONY: tidy
tidy: ## Tidy go modules
	go mod tidy

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/

.PHONY: pre-commit
pre-commit: sqlc-verify test lint ## Run pre-commit checks

.PHONY: install-tools
install-tools: ## Install development tools
	@echo "Installing sqlc..."
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	@echo "Installing golang-migrate..."
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "Installing golangci-lint..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Done!"
