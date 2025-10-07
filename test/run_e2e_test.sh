#!/bin/bash
#
# CyberMesh E2E Test Runner
# Runs end-to-end architecture test
#
# Usage: ./test/run_e2e_test.sh
#

set -e

echo "=========================================================================="
echo "CyberMesh End-to-End Architecture Test"
echo "=========================================================================="
echo ""

# Check if we're in the right directory
if [ ! -d "ai-service" ] || [ ! -d "backend" ]; then
    echo "Error: Must run from CyberMesh root directory"
    echo "Usage: cd /mnt/b/CyberMesh && ./test/run_e2e_test.sh"
    exit 1
fi

# Check Python
if ! command -v python3 &> /dev/null; then
    echo "Error: Python 3 not found"
    exit 1
fi

# Check backend binary
if [ ! -f "backend/bin/backend.exe" ]; then
    echo "Error: Backend binary not found at backend/bin/backend.exe"
    echo "Please build backend first: cd backend && go build -o bin/backend.exe ./cmd/cybermesh/main.go"
    exit 1
fi

# Check AI service
if [ ! -f "ai-service/.env" ]; then
    echo "Error: AI service .env not found"
    exit 1
fi

echo "Prerequisites check: OK"
echo ""
echo "Running E2E test..."
echo ""

# Run test
cd /mnt/b/CyberMesh
python3 test/test_e2e_architecture.py

exit_code=$?

echo ""
if [ $exit_code -eq 0 ]; then
    echo "=========================================================================="
    echo "E2E Test Completed Successfully"
    echo "=========================================================================="
else
    echo "=========================================================================="
    echo "E2E Test Failed (exit code: $exit_code)"
    echo "=========================================================================="
fi

exit $exit_code
