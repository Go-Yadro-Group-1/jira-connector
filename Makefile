.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make generate    - Generate code from proto files"
	@echo "  make tidy        - Run go mod tidy"
	@echo "  make test        - Run all tests"
	@echo "  make test-cover  - Run tests with coverage"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make all         - Run tidy, generate, test"

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

.PHONY: clean
clean:
	@echo "Cleaning generated files..."
	rm -rf gen/
	@echo "Done."

.PHONY: all
all: tidy generate test
	@echo "All tasks completed."
