# CyberMesh Backend Makefile

.PHONY: test test-unit test-integration test-coverage test-race clean build build-plugin build-plugin-win build-plugin-linux release-plugin plugin-completions plugin-installers plugin-checksums plugin-sign plugin-verify

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

# Build kubectl plugin
build-plugin:
	go build -o bin/kubectl-cybermesh ./cmd/kubectl-cybermesh

build-plugin-win:
	go build -o bin/kubectl-cybermesh.exe ./cmd/kubectl-cybermesh

build-plugin-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/kubectl-cybermesh ./cmd/kubectl-cybermesh

plugin-completions: build-plugin-win build-plugin-linux
	./bin/kubectl-cybermesh completion bash > bin/kubectl-cybermesh.bash
	./bin/kubectl-cybermesh completion zsh > bin/_kubectl-cybermesh
	powershell -NoProfile -Command "& .\\bin\\kubectl-cybermesh.exe completion powershell" > bin/kubectl-cybermesh.ps1

plugin-installers:
	cp cmd/kubectl-cybermesh/install/install.ps1 bin/install-kubectl-cybermesh.ps1
	cp cmd/kubectl-cybermesh/install/uninstall.ps1 bin/uninstall-kubectl-cybermesh.ps1
	cp cmd/kubectl-cybermesh/install/install.sh bin/install-kubectl-cybermesh.sh
	cp cmd/kubectl-cybermesh/install/uninstall.sh bin/uninstall-kubectl-cybermesh.sh

plugin-checksums: plugin-installers plugin-completions
	powershell -NoProfile -ExecutionPolicy Bypass -File cmd/kubectl-cybermesh/release/generate-checksums.ps1 -ArtifactsDir bin

plugin-sign:
	powershell -NoProfile -ExecutionPolicy Bypass -File cmd/kubectl-cybermesh/release/sign-release.ps1 -ArtifactsDir bin

plugin-verify:
	powershell -NoProfile -ExecutionPolicy Bypass -File cmd/kubectl-cybermesh/release/verify-release.ps1 -ArtifactsDir bin

release-plugin: build-plugin-win build-plugin-linux plugin-completions plugin-installers plugin-checksums
	@echo "Plugin artifacts written to backend/bin"

# Run linters
lint:
	go vet ./...
	gofmt -s -w .

# Run all checks before commit
pre-commit: lint test-race
	@echo "All checks passed!"
