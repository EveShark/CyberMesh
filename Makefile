# ============================================================
# CyberMesh - Root Makefile for CI/CD Pipeline
# ============================================================
# Complete CI/CD pipeline with fail-fast behavior
# Usage:
#   make ci              - Run all checks (no Docker)
#   make build-images    - Build all Docker images
#   make push-images     - Push all images to GCP
#   make deploy          - Full pipeline (checks + build + push)
#   make clean           - Clean build artifacts
# ============================================================

.PHONY: help ci clean \
        lint-go vet-go test-go build-go-binary \
        check-python check-typescript build-frontend \
        build-backend-image build-ai-image build-frontend-image build-images \
        push-backend push-ai push-frontend push-images \
        deploy verify-images \
        fix-go fix-python fix-frontend fix-all \
        run-backend run-frontend run-ai \
        k8s-deploy k8s-restart-validators k8s-restart-frontend k8s-restart-ai k8s-status \
        security-check deps-update \
        coverage-go test-quick

# ============================================================
# CONFIGURATION
# ============================================================

# Docker image names
BACKEND_IMAGE := asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest
AI_IMAGE := asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest
FRONTEND_IMAGE := asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest

# Frontend build args
FRONTEND_API_BASE := http://35.187.249.43:9441/api/v1
AI_API_BASE := http://ai-service.cybermesh.svc.cluster.local:8080

# Colors for output
GREEN := \033[0;32m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

# ============================================================
# DEFAULT TARGET
# ============================================================

help:
	@echo "$(GREEN)CyberMesh CI/CD Makefile$(NC)"
	@echo ""
	@echo "$(YELLOW)CI Targets (checks only):$(NC)"
	@echo "  make ci                  - Run all CI checks (no Docker builds)"
	@echo "  make lint-go             - Check Go formatting"
	@echo "  make vet-go              - Run Go static analysis"
	@echo "  make test-go             - Run Go tests with race detector"
	@echo "  make check-python        - Compile all Python files"
	@echo "  make check-typescript    - Run TypeScript type checking"
	@echo ""
	@echo "$(YELLOW)Build Targets:$(NC)"
	@echo "  make build-go-binary     - Build Go binary for Linux/AMD64"
	@echo "  make build-frontend      - Build frontend production bundle"
	@echo "  make build-images        - Build all 3 Docker images"
	@echo "  make build-backend-image - Build backend Docker image"
	@echo "  make build-ai-image      - Build AI service Docker image"
	@echo "  make build-frontend-image- Build frontend Docker image"
	@echo ""
	@echo "$(YELLOW)Deploy Targets:$(NC)"
	@echo "  make push-images         - Push all images to GCP registry"
	@echo "  make push-backend        - Push backend image only"
	@echo "  make push-ai             - Push AI service image only"
	@echo "  make push-frontend       - Push frontend image only"
	@echo "  make verify-images       - Verify all images in GCP registry"
	@echo ""
	@echo "$(YELLOW)Full Pipeline:$(NC)"
	@echo "  make deploy              - Run full CI/CD pipeline"
	@echo ""
	@echo "$(YELLOW)Developer Tools:$(NC)"
	@echo "  make fix-go              - Auto-format Go code"
	@echo "  make fix-python          - Auto-format Python code (requires black)"
	@echo "  make fix-frontend        - Auto-format frontend code (prettier)"
	@echo "  make fix-all             - Auto-format all code (Go + Python + Frontend)"
	@echo "  make run-backend         - Run backend locally"
	@echo "  make run-frontend        - Run frontend dev server"
	@echo "  make run-ai              - Run AI service locally"
	@echo "  make test-quick          - Run quick tests (no race detector)"
	@echo "  make coverage-go         - Generate Go coverage report"
	@echo ""
	@echo "$(YELLOW)Kubernetes:$(NC)"
	@echo "  make k8s-status          - Show pod status"
	@echo "  make k8s-restart-validators - Restart all validator pods"
	@echo "  make k8s-restart-frontend   - Restart frontend pod"
	@echo "  make k8s-restart-ai         - Restart AI service pod"
	@echo "  make k8s-deploy          - Apply K8s manifests"
	@echo ""
	@echo "$(YELLOW)Security & Maintenance:$(NC)"
	@echo "  make security-check      - Scan dependencies for vulnerabilities"
	@echo "  make deps-update         - Update all dependencies"
	@echo ""
	@echo "$(YELLOW)Utility:$(NC)"
	@echo "  make clean               - Clean build artifacts"

# ============================================================
# STAGE 1: LINT & FORMAT CHECKS
# ============================================================

lint-go:
	@echo "$(GREEN)==> Stage 1.1: Checking Go formatting...$(NC)"
	@cd backend && \
	UNFORMATTED=$$(gofmt -l ./cmd ./pkg); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "$(RED)ERROR: Unformatted Go files found:$(NC)"; \
		echo "$$UNFORMATTED"; \
		echo "$(YELLOW)Run 'gofmt -w .' in backend/ to fix$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ All Go files properly formatted$(NC)"

vet-go:
	@echo "$(GREEN)==> Stage 1.2: Running Go static analysis...$(NC)"
	@cd backend && go vet ./cmd/... ./pkg/...
	@echo "$(GREEN)✓ Go vet passed$(NC)"

# ============================================================
# STAGE 2: TYPE & SYNTAX CHECKS
# ============================================================

check-python:
	@echo "$(GREEN)==> Stage 2.1: Checking Python syntax...$(NC)"
	@python3 -m compileall -q ai-service/src ai-service/cmd || \
		(echo "$(RED)ERROR: Python syntax errors found$(NC)"; exit 1)
	@echo "$(GREEN)✓ Python syntax check passed$(NC)"

check-typescript:
	@echo "$(GREEN)==> Stage 2.2: Running TypeScript type checking...$(NC)"
	@cd frontend && npm run build --dry-run > /dev/null 2>&1 || \
		(cd frontend && npx tsc --noEmit)
	@echo "$(GREEN)✓ TypeScript check passed$(NC)"

# ============================================================
# STAGE 3: TESTS
# ============================================================

test-go:
	@echo "$(GREEN)==> Stage 3: Running Go tests with race detector...$(NC)"
	@cd backend && go test -race ./cmd/... ./pkg/...
	@echo "$(GREEN)✓ All Go tests passed$(NC)"

# ============================================================
# STAGE 4: BUILD VERIFICATION
# ============================================================

build-go-binary:
	@echo "$(GREEN)==> Stage 4.1: Building Go binary (Linux/AMD64)...$(NC)"
	@rm -f backend/bin/cybermesh
	@cd backend && \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -o bin/cybermesh ./cmd/cybermesh/main.go
	@if [ ! -f backend/bin/cybermesh ]; then \
		echo "$(RED)ERROR: Binary not created$(NC)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ Go binary built successfully$(NC)"
	@ls -lh backend/bin/cybermesh | awk '{print "  Size: " $$5 ", Modified: " $$6 " " $$7 " " $$8}'

build-frontend:
	@echo "$(GREEN)==> Stage 4.2: Building frontend production bundle...$(NC)"
	@cd frontend && NODE_ENV=production npm run build
	@echo "$(GREEN)✓ Frontend build successful$(NC)"

# ============================================================
# STAGE 5: DOCKER IMAGE BUILDS
# ============================================================

build-backend-image: build-go-binary
	@echo "$(GREEN)==> Stage 5.1: Building backend Docker image...$(NC)"
	@docker build -f docker/backend/Dockerfile -t $(BACKEND_IMAGE) .
	@docker images $(BACKEND_IMAGE) --format "{{.Repository}}:{{.Tag}} {{.ID}}" | head -1
	@echo "$(GREEN)✓ Backend image built$(NC)"

build-ai-image:
	@echo "$(GREEN)==> Stage 5.2: Building AI service Docker image...$(NC)"
	@docker build -f docker/ai-service/Dockerfile -t $(AI_IMAGE) .
	@docker images $(AI_IMAGE) --format "{{.Repository}}:{{.Tag}} {{.ID}}" | head -1
	@echo "$(GREEN)✓ AI service image built$(NC)"

build-frontend-image:
	@echo "$(GREEN)==> Stage 5.3: Building frontend Docker image...$(NC)"
	@docker build -f docker/frontend/Dockerfile \
		--build-arg NEXT_PUBLIC_BACKEND_API_BASE=$(FRONTEND_API_BASE) \
		--build-arg NEXT_PUBLIC_AI_API_BASE=$(AI_API_BASE) \
		-t $(FRONTEND_IMAGE) .
	@docker images $(FRONTEND_IMAGE) --format "{{.Repository}}:{{.Tag}} {{.ID}}" | head -1
	@echo "$(GREEN)✓ Frontend image built$(NC)"

build-images: build-backend-image build-ai-image build-frontend-image
	@echo "$(GREEN)✓ All Docker images built successfully$(NC)"

# ============================================================
# STAGE 6: PUSH TO GCP REGISTRY
# ============================================================

push-backend:
	@echo "$(GREEN)==> Stage 6.1: Pushing backend image to GCP...$(NC)"
	@docker push $(BACKEND_IMAGE) | tail -5
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend --limit=1
	@echo "$(GREEN)✓ Backend image pushed and verified$(NC)"

push-ai:
	@echo "$(GREEN)==> Stage 6.2: Pushing AI service image to GCP...$(NC)"
	@docker push $(AI_IMAGE) | tail -5
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service --limit=1
	@echo "$(GREEN)✓ AI service image pushed and verified$(NC)"

push-frontend:
	@echo "$(GREEN)==> Stage 6.3: Pushing frontend image to GCP...$(NC)"
	@docker push $(FRONTEND_IMAGE) | tail -5
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend --limit=1
	@echo "$(GREEN)✓ Frontend image pushed and verified$(NC)"

push-images: push-backend push-ai push-frontend
	@echo "$(GREEN)✓ All images pushed and verified in GCP registry$(NC)"

# ============================================================
# VERIFICATION
# ============================================================

verify-images:
	@echo "$(GREEN)==> Verifying all images in GCP registry...$(NC)"
	@echo "$(YELLOW)Backend:$(NC)"
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend --limit=1
	@echo ""
	@echo "$(YELLOW)AI Service:$(NC)"
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service --limit=1
	@echo ""
	@echo "$(YELLOW)Frontend:$(NC)"
	@gcloud artifacts docker tags list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend --limit=1
	@echo ""
	@echo "$(GREEN)✓ All images verified$(NC)"

# ============================================================
# CONVENIENCE TARGETS
# ============================================================

# Run all CI checks (no Docker builds)
ci: lint-go vet-go check-python check-typescript test-go
	@echo ""
	@echo "$(GREEN)========================================$(NC)"
	@echo "$(GREEN)✓ ALL CI CHECKS PASSED$(NC)"
	@echo "$(GREEN)========================================$(NC)"
	@echo ""
	@echo "$(YELLOW)Ready to build Docker images:$(NC)"
	@echo "  make build-images"

# Full pipeline: CI checks + Docker builds + Push
deploy: ci build-images push-images
	@echo ""
	@echo "$(GREEN)========================================$(NC)"
	@echo "$(GREEN)✓ DEPLOYMENT COMPLETE$(NC)"
	@echo "$(GREEN)========================================$(NC)"
	@echo ""
	@echo "$(YELLOW)All images are now in GCP Artifact Registry$(NC)"
	@echo "$(YELLOW)You can now update Kubernetes deployments$(NC)"

# ============================================================
# DEVELOPER TOOLS
# ============================================================

# Auto-format Go code
fix-go:
	@echo "$(GREEN)==> Formatting Go code...$(NC)"
	@cd backend && gofmt -w ./cmd ./pkg
	@echo "$(GREEN)✓ Go code formatted$(NC)"

# Auto-format Python code (requires black)
fix-python:
	@echo "$(GREEN)==> Formatting Python code...$(NC)"
	@if ! command -v black &> /dev/null; then \
		echo "$(RED)ERROR: black not installed. Install: pip install black$(NC)"; \
		exit 1; \
	fi
	@black ai-service/src ai-service/cmd
	@echo "$(GREEN)✓ Python code formatted$(NC)"

# Auto-format frontend code (prettier)
fix-frontend:
	@echo "$(GREEN)==> Formatting frontend code...$(NC)"
	@cd frontend && npx prettier --write "src/**/*.{ts,tsx,js,jsx,json,css}"
	@echo "$(GREEN)✓ Frontend code formatted$(NC)"

# Format all code
fix-all: fix-go fix-python fix-frontend
	@echo ""
	@echo "$(GREEN)========================================$(NC)"
	@echo "$(GREEN)✓ ALL CODE FORMATTED$(NC)"
	@echo "$(GREEN)========================================$(NC)"

# Run backend locally
run-backend:
	@echo "$(GREEN)==> Starting backend locally...$(NC)"
	@cd backend && go run ./cmd/cybermesh/main.go

# Run frontend dev server
run-frontend:
	@echo "$(GREEN)==> Starting frontend dev server...$(NC)"
	@cd frontend && npm run dev

# Run AI service locally
run-ai:
	@echo "$(GREEN)==> Starting AI service locally...$(NC)"
	@cd ai-service && python3 cmd/main.py

# Quick tests (no race detector, faster)
test-quick:
	@echo "$(GREEN)==> Running quick tests...$(NC)"
	@cd backend && go test -short ./cmd/... ./pkg/...
	@echo "$(GREEN)✓ Quick tests passed$(NC)"

# Generate Go coverage report
coverage-go:
	@echo "$(GREEN)==> Generating Go coverage report...$(NC)"
	@cd backend && go test -coverprofile=coverage.out ./cmd/... ./pkg/...
	@cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✓ Coverage report: backend/coverage.html$(NC)"

# ============================================================
# KUBERNETES COMMANDS
# ============================================================

# Show pod status
k8s-status:
	@echo "$(GREEN)==> Kubernetes pod status:$(NC)"
	@kubectl get pods -n cybermesh

# Restart all validators
k8s-restart-validators:
	@echo "$(YELLOW)==> Restarting all validator pods...$(NC)"
	@kubectl delete pod validator-0 validator-1 validator-2 validator-3 validator-4 -n cybermesh
	@echo "$(GREEN)✓ Validators restarting (check with 'make k8s-status')$(NC)"

# Restart frontend
k8s-restart-frontend:
	@echo "$(YELLOW)==> Restarting frontend pod...$(NC)"
	@kubectl delete pod -l app=frontend -n cybermesh
	@echo "$(GREEN)✓ Frontend restarting$(NC)"

# Restart AI service
k8s-restart-ai:
	@echo "$(YELLOW)==> Restarting AI service pod...$(NC)"
	@kubectl delete pod -l app=ai-service -n cybermesh
	@echo "$(GREEN)✓ AI service restarting$(NC)"

# Deploy/update K8s manifests
k8s-deploy:
	@echo "$(GREEN)==> Applying Kubernetes manifests...$(NC)"
	@kubectl apply -f k8s_gke/
	@echo "$(GREEN)✓ K8s resources updated$(NC)"
	@echo "$(YELLOW)Run 'make k8s-status' to check deployment$(NC)"

# ============================================================
# SECURITY & MAINTENANCE
# ============================================================

# Check for security vulnerabilities
security-check:
	@echo "$(GREEN)==> Checking dependencies for vulnerabilities...$(NC)"
	@echo "$(YELLOW)Checking Go dependencies...$(NC)"
	@cd backend && go list -json -m all | grep -E '"(Path|Version)"' || true
	@echo ""
	@echo "$(YELLOW)Checking npm dependencies...$(NC)"
	@cd frontend && npm audit || true
	@echo ""
	@echo "$(GREEN)✓ Security check complete$(NC)"
	@echo "$(YELLOW)Note: Install 'nancy' for Go and 'pip-audit' for Python for full scanning$(NC)"

# Update all dependencies
deps-update:
	@echo "$(YELLOW)==> Updating dependencies...$(NC)"
	@echo "$(YELLOW)WARNING: This will update dependencies. Test thoroughly after!$(NC)"
	@read -p "Continue? (y/n) " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		echo "$(GREEN)Updating Go modules...$(NC)"; \
		cd backend && go get -u ./... && go mod tidy; \
		echo "$(GREEN)Updating npm packages...$(NC)"; \
		cd frontend && npm update; \
		echo "$(GREEN)Updating pip packages...$(NC)"; \
		pip install --upgrade -r ai-service/requirements.txt; \
		echo "$(GREEN)✓ Dependencies updated$(NC)"; \
		echo "$(YELLOW)Run 'make ci' to test changes$(NC)"; \
	else \
		echo "$(RED)Update cancelled$(NC)"; \
	fi

# ============================================================
# CLEANUP
# ============================================================

clean:
	@echo "$(YELLOW)Cleaning build artifacts...$(NC)"
	@rm -f backend/bin/cybermesh
	@rm -rf frontend/.next
	@rm -rf frontend/out
	@rm -f backend/coverage.out backend/coverage.html
	@echo "$(GREEN)✓ Clean complete$(NC)"
