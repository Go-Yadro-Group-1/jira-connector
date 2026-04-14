.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make generate    - Generate code from proto files"
	@echo "  make tidy        - Run go mod tidy"
	@echo "  make test        - Run all tests"
	@echo "  make test-cover  - Run tests with coverage"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make build       - Build server and client binaries"
	@echo "  make build-server - Build server binary"
	@echo "  make build-client - Build client binary"
	@echo "  make run         - Run server with default config"
	@echo "  make run-client  - Run test client"
	@echo "  make run-sync    - Run sync for specific project (PROJECT=<key>)"
	@echo "  make db-up       - Start PostgreSQL (docker-compose)"
	@echo "  make db-down     - Stop PostgreSQL (docker-compose)"
	@echo "  make db-restart  - Restart PostgreSQL (docker-compose)"
	@echo "  make db-logs     - Show PostgreSQL logs (docker-compose)"
	@echo "  make db-status   - Show PostgreSQL status"
	@echo "  make db-psql     - Connect to PostgreSQL shell"
	@echo "  make clean       - Clean binaries and generated files"
	@echo "  make all         - Run tidy, generate, test, build"

.PHONY: generate
generate:
	@echo "Generating code from proto files..."
	cd api/proto && go generate
	@echo "Done."

.PHONY: tidy
tidy:
	@echo "Running go mod tidy..."
	go mod tidy
	@echo "Done."

.PHONY: test
test:
	@echo "Running tests..."
	go test -v ./...

.PHONY: test-cover
test-cover:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | grep total
	@echo "Coverage report saved to coverage.out"

.PHONY: lint
lint:
	@echo "Running linter..."
	golangci-lint run ./...

BIN_DIR := bin

.PHONY: build-server
build-server:
	@echo "Building server..."
	go build -o $(BIN_DIR)/jira-connector-server ./cmd/main.go
	@echo "Server binary saved to $(BIN_DIR)/jira-connector-server"

.PHONY: build-client
build-client:
	@echo "Building client..."
	go build -o $(BIN_DIR)/jira-connector-client ./cmd/internal/cli/client/
	@echo "Client binary saved to $(BIN_DIR)/jira-connector-client"

.PHONY: build
build: build-server build-client
	@echo "All binaries built."

.PHONY: run
run: build-server
	@echo "Starting server..."
	$(BIN_DIR)/jira-connector-server serve

.PHONY: run-client
run-client: build-client
	@echo "Starting client..."
	$(BIN_DIR)/jira-connector-client

.PHONY: run-sync
run-sync: build-server
ifndef PROJECT
	$(error PROJECT is not set. Usage: make run-sync PROJECT=DEMO)
endif
	@echo "Syncing project $(PROJECT)..."
	$(BIN_DIR)/jira-connector-server run --project $(PROJECT)

.PHONY: clean
clean:
	@echo "Cleaning binaries..."
	rm -rf $(BIN_DIR)/*
	@echo "Done."

# === Database ===
# Использует docker compose v2 (плагин). Для sudo: sudo make db-up
DOCKER_COMPOSE = docker compose

.PHONY: db-up
db-up:
	@echo "Starting PostgreSQL..."
	sudo $(DOCKER_COMPOSE) up -d postgres
	@echo "Waiting for database to be ready..."
	@until sudo $(DOCKER_COMPOSE) exec -T postgres pg_isready -U $${DB_USER:-postgres} -d $${DB_NAME:-jira_connector} >/dev/null 2>&1; do \
		sleep 1; \
	done
	@echo "Database is ready!"

.PHONY: db-down
db-down:
	@echo "Stopping PostgreSQL..."
	sudo $(DOCKER_COMPOSE) down

.PHONY: db-restart
db-restart: db-down db-up

.PHONY: db-logs
db-logs:
	sudo $(DOCKER_COMPOSE) logs -f postgres

.PHONY: db-status
db-status:
	@echo "PostgreSQL container status:"
	sudo $(DOCKER_COMPOSE) ps postgres

.PHONY: db-psql
db-psql:
	sudo $(DOCKER_COMPOSE) exec postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-jira_connector}

.PHONY: db-reset
db-reset:
	@echo "Dropping and recreating raw schema..."
	@sudo $(DOCKER_COMPOSE) exec -T postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-jira_connector} -c "DROP SCHEMA IF EXISTS raw CASCADE;"
	@echo "Schema dropped. Tables will be recreated on next server start."

.PHONY: db-check
db-check:
	@echo "Checking database connection and tables..."
	@sudo $(DOCKER_COMPOSE) exec -T postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-jira_connector} -c "\dt raw.*" 2>/dev/null || echo "Tables not found"
	@echo ""
	@echo "Row counts:"
	@sudo $(DOCKER_COMPOSE) exec -T postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-jira_connector} -c "SELECT 'projects' AS tbl, count(*) FROM raw.project UNION ALL SELECT 'authors', count(*) FROM raw.author UNION ALL SELECT 'issues', count(*) FROM raw.issue UNION ALL SELECT 'status_changes', count(*) FROM raw.status_changes;" 2>/dev/null || echo "Tables not created yet"

.PHONY: all
all: tidy generate test build
	@echo "All tasks completed."
