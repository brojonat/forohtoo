#!/usr/bin/env bash

# Set the shell for make explicitly
SHELL := /bin/bash

define setup_env
        $(eval ENV_FILE := $(1))
        $(eval include $(1))
        $(eval export)
endef

.PHONY: help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: test
test: ## Run unit tests (skips integration tests)
	go test ./... -v -race -cover

.PHONY: test-db
test-db: ## Run database integration tests (requires postgres-test)
	RUN_DB_TESTS=1 go test -v ./cmd/forohtoo -run 'TestList|TestGet|TestTransaction'

.PHONY: test-temporal
test-temporal: ## Run Temporal integration tests (requires temporal)
	RUN_TEMPORAL_TESTS=1 go test -v ./cmd/forohtoo -run 'TestTemporal|TestPause|TestResume|TestDelete|TestCreate|TestDescribe'

.PHONY: test-integration
test-integration: ## Run all integration tests (requires all services)
	$(call setup_env, .env.test)
	RUN_DB_TESTS=1 RUN_TEMPORAL_TESTS=1 go test ./... -v -p 1

.PHONY: lint
lint: ## Run linter
	golangci-lint run

.PHONY: build-server
build-server: ## Build HTTP server binary
	go build -o bin/server ./cmd/server

.PHONY: build-worker
build-worker: ## Build Temporal worker binary
	go build -o bin/worker ./cmd/worker

.PHONY: build-cli
build-cli: ## Build CLI binary
	go build -o bin/forohtoo ./cmd/forohtoo

.PHONY: build
build: build-server build-worker build-cli ## Build all binaries

.PHONY: run-server
run-server: build-server ## Build and run the HTTP server
	./bin/server

.PHONY: run-worker
run-worker: build-worker ## Build and run the Temporal worker
	./bin/worker

.PHONY: run
run: build ## Build and run both server and worker
	@echo "Starting server and worker..."
	@echo "Run 'make run-server' or 'make run-worker' to run them individually"
	@echo "Or use tmux/docker-compose to run both simultaneously"

.PHONY: run-dev
run-dev: ## Run server with hot reload using air
	air

.PHONY: start-dev-session
start-dev-session: ## Start tmux development session
	./scripts/dev.sh

.PHONY: stop-dev-session
stop-dev-session: ## Stop tmux development session
	tmux kill-session -t forohtoo || true

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

.PHONY: docker-up
docker-up: ## Start Docker services (Postgres, NATS, Temporal)
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

# Kubernetes/Docker targets
.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t forohtoo:latest .

.PHONY: docker-build-tag
docker-build-tag: ## Build and tag Docker image (requires DOCKER_REPO and GIT_COMMIT_SHA)
	@if [ -z "$(DOCKER_REPO)" ]; then echo "Error: DOCKER_REPO not set"; exit 1; fi
	@if [ -z "$(GIT_COMMIT_SHA)" ]; then echo "Error: GIT_COMMIT_SHA not set"; exit 1; fi
	docker build -t $(DOCKER_REPO)/forohtoo:$(GIT_COMMIT_SHA) .
	docker tag $(DOCKER_REPO)/forohtoo:$(GIT_COMMIT_SHA) $(DOCKER_REPO)/forohtoo:latest

.PHONY: docker-push
docker-push: ## Push Docker image (requires DOCKER_REPO and GIT_COMMIT_SHA)
	@if [ -z "$(DOCKER_REPO)" ]; then echo "Error: DOCKER_REPO not set"; exit 1; fi
	@if [ -z "$(GIT_COMMIT_SHA)" ]; then echo "Error: GIT_COMMIT_SHA not set"; exit 1; fi
	docker push $(DOCKER_REPO)/forohtoo:$(GIT_COMMIT_SHA)
	docker push $(DOCKER_REPO)/forohtoo:latest

.PHONY: k8s-apply
k8s-apply: ## Apply Kubernetes manifests (requires DOCKER_REPO and GIT_COMMIT_SHA)
	@if [ -z "$(DOCKER_REPO)" ]; then echo "Error: DOCKER_REPO not set"; exit 1; fi
	@if [ -z "$(GIT_COMMIT_SHA)" ]; then echo "Error: GIT_COMMIT_SHA not set"; exit 1; fi
	@echo "Applying Kubernetes manifests..."
	@cat k8s/prod/server.yaml | sed 's|{{DOCKER_REPO}}|$(DOCKER_REPO)|g' | sed 's|{{GIT_COMMIT_SHA}}|$(GIT_COMMIT_SHA)|g' | kubectl apply -f -
	@cat k8s/prod/worker.yaml | sed 's|{{DOCKER_REPO}}|$(DOCKER_REPO)|g' | sed 's|{{GIT_COMMIT_SHA}}|$(GIT_COMMIT_SHA)|g' | kubectl apply -f -

.PHONY: k8s-apply-kustomize
k8s-apply-kustomize: ## Apply Kubernetes manifests using kustomize (requires .env files)
	@if [ ! -f .env.server.prod ]; then echo "Error: .env.server.prod not found"; exit 1; fi
	@if [ ! -f .env.worker.prod ]; then echo "Error: .env.worker.prod not found"; exit 1; fi
	kubectl apply -k k8s/prod

.PHONY: k8s-delete
k8s-delete: ## Delete Kubernetes resources
	kubectl delete -k k8s/prod

.PHONY: k8s-logs-server
k8s-logs-server: ## Show server logs
	kubectl logs -l app=forohtoo-server -f

.PHONY: k8s-logs-worker
k8s-logs-worker: ## Show worker logs
	kubectl logs -l app=forohtoo-worker -f

.PHONY: k8s-restart-server
k8s-restart-server: ## Restart server deployment
	kubectl rollout restart deployment/forohtoo-server

.PHONY: k8s-restart-worker
k8s-restart-worker: ## Restart worker deployment
	kubectl rollout restart deployment/forohtoo-worker

.PHONY: k8s-status
k8s-status: ## Show Kubernetes deployment status
	@echo "=== Deployments ==="
	kubectl get deployments -l app=forohtoo-server -o wide
	kubectl get deployments -l app=forohtoo-worker -o wide
	@echo "\n=== Pods ==="
	kubectl get pods -l app=forohtoo-server
	kubectl get pods -l app=forohtoo-worker
	@echo "\n=== Services ==="
	kubectl get services -l app=forohtoo-server

.PHONY: deploy
deploy: docker-build-tag docker-push k8s-apply ## Full deployment: build, push, and apply (requires DOCKER_REPO and GIT_COMMIT_SHA)
	@echo "Deployment complete!"
	@echo "Monitor status with: make k8s-status"
