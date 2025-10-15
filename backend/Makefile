# CyberMesh Backend Makefile

.PHONY: test test-unit test-integration test-coverage test-race clean build

# Default: run all unit tests
test: test-unit

# Unit tests (fast, no external deps)
test-unit:
	@echo "Running unit tests..."
	go test -short -v ./pkg/state/... ./pkg/block/... ./pkg/mempool/... ./pkg/utils/...

# Integration tests (requires CockroachDB, Kafka)
test-integration:
	@echo "Running integration tests..."
	go test -v ./tests/integration/...

# All tests
test-all:
	@echo "Running all tests..."
	go test -v ./...

# Tests with race detector
test-race:
	@echo "Running tests with race detector..."
	go test -race ./...

# Coverage report
test-coverage:
	@echo "Generating coverage report..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean test artifacts
clean:
	rm -f coverage.out coverage.html

# Build backend
build:
	go build -o bin/cybermesh ./cmd/cybermesh

# Run linters
lint:
	go vet ./...
	gofmt -s -w .

# Run all checks before commit
pre-commit: lint test-race
	@echo "All checks passed!"
