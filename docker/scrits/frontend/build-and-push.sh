#!/bin/bash
# ============================================================
# Build and Push CyberMesh Frontend Docker Image
# ============================================================
# Usage: ./build-and-push.sh [tag]
# Example: ./build-and-push.sh v1.0.0
# ============================================================

set -e

# Configuration
IMAGE_NAME="cybermesh-frontend"
REGISTRY="${REGISTRY:-cybermeshrg.azurecr.io}"
TAG="${1:-latest}"
FULL_IMAGE="${REGISTRY}/${IMAGE_NAME}:${TAG}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Building CyberMesh Frontend Docker Image ===${NC}"
echo -e "${YELLOW}Image: ${FULL_IMAGE}${NC}"
echo ""

# Navigate to project root
cd "$(dirname "$0")/../.."

# Load environment variables from .env
if [ -f "cybermesh-frontend/.env" ]; then
    echo -e "${GREEN}Loading environment variables from .env${NC}"
    source cybermesh-frontend/.env
else
    echo -e "${RED}Error: .env file not found at cybermesh-frontend/.env${NC}"
    exit 1
fi

# Build Docker image with build arguments
echo -e "${GREEN}Building Docker image...${NC}"
docker build \
    -f docker/frontend/Dockerfile.vite \
    -t "${FULL_IMAGE}" \
    --build-arg VITE_SUPABASE_PROJECT_ID="${VITE_SUPABASE_PROJECT_ID}" \
    --build-arg VITE_SUPABASE_URL="${VITE_SUPABASE_URL}" \
    --build-arg VITE_SUPABASE_PUBLISHABLE_KEY="${VITE_SUPABASE_PUBLISHABLE_KEY}" \
    --build-arg VITE_DEMO_MODE="${VITE_DEMO_MODE:-false}" \
    --build-arg VITE_BACKEND_URL="${VITE_BACKEND_URL}" \
    --build-arg VITE_LANDING_URL="${VITE_LANDING_URL:-/}" \
    .

echo -e "${GREEN}✓ Build complete!${NC}"
echo ""

# Tag as latest
if [ "$TAG" != "latest" ]; then
    echo -e "${GREEN}Tagging as latest...${NC}"
    docker tag "${FULL_IMAGE}" "${REGISTRY}/${IMAGE_NAME}:latest"
fi

# Push to registry
echo -e "${YELLOW}Push to registry? (y/n)${NC}"
read -r response
if [[ "$response" =~ ^([yY][eE][sS]|[yY])$ ]]; then
    echo -e "${GREEN}Pushing to registry...${NC}"
    docker push "${FULL_IMAGE}"
    
    if [ "$TAG" != "latest" ]; then
        docker push "${REGISTRY}/${IMAGE_NAME}:latest"
    fi
    
    echo -e "${GREEN}✓ Push complete!${NC}"
    echo ""
    echo -e "${GREEN}Image available at:${NC}"
    echo -e "  ${FULL_IMAGE}"
    
    if [ "$TAG" != "latest" ]; then
        echo -e "  ${REGISTRY}/${IMAGE_NAME}:latest"
    fi
else
    echo -e "${YELLOW}Skipping push${NC}"
fi

echo ""
echo -e "${GREEN}=== Summary ===${NC}"
echo -e "Image: ${FULL_IMAGE}"
echo -e "Size: $(docker images ${FULL_IMAGE} --format "{{.Size}}")"
echo ""
echo -e "${GREEN}To run locally:${NC}"
echo -e "  docker run -p 8080:8080 ${FULL_IMAGE}"
