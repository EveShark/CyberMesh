#!/usr/bin/env pwsh
# Script to prepare K8s manifests for GKE deployment

param(
    [Parameter(Mandatory=$true)]
    [string]$GCPProjectID,
    
    [Parameter(Mandatory=$false)]
    [string]$ImageTag = "v1.0.0",
    
    [Parameter(Mandatory=$false)]
    [switch]$DryRun
)

Write-Host "=== CyberMesh GKE Preparation Script ===" -ForegroundColor Cyan
Write-Host ""

$k8sDir = $PSScriptRoot
$configMapFile = Join-Path $k8sDir "configmap.yaml"
$statefulSetFile = Join-Path $k8sDir "statefulset.yaml"
$backupDir = Join-Path $k8sDir "backups"

# Create backup directory
if (-not (Test-Path $backupDir)) {
    New-Item -ItemType Directory -Path $backupDir | Out-Null
}

$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"

# Step 1: Backup existing files
Write-Host "1. Creating backups..." -ForegroundColor Yellow
Copy-Item $configMapFile (Join-Path $backupDir "configmap_${timestamp}.yaml")
Copy-Item $statefulSetFile (Join-Path $backupDir "statefulset_${timestamp}.yaml")
Write-Host "   ✓ Backups created in $backupDir" -ForegroundColor Green

# Step 2: Update ConfigMap
Write-Host ""
Write-Host "2. Updating ConfigMap..." -ForegroundColor Yellow

$configMapContent = Get-Content $configMapFile -Raw

# Change ENVIRONMENT to production
$configMapContent = $configMapContent -replace 'ENVIRONMENT: "development"', 'ENVIRONMENT: "production"'
Write-Host "   ✓ ENVIRONMENT: development → production" -ForegroundColor Green

# Enable API TLS
$configMapContent = $configMapContent -replace 'API_TLS_ENABLED: "false"', 'API_TLS_ENABLED: "true"'
Write-Host "   ✓ API_TLS_ENABLED: false → true" -ForegroundColor Green

# Remove DB_DSN from ConfigMap (security risk - already in Secret)
if ($configMapContent -match '  DB_DSN: "postgresql://[^"]*"') {
    Write-Host "   ⚠ WARNING: DB_DSN found in ConfigMap (should only be in Secret)" -ForegroundColor Red
    Write-Host "   ! Commenting out DB_DSN line" -ForegroundColor Yellow
    $configMapContent = $configMapContent -replace '(  DB_DSN: "postgresql://[^"]*")', '# REMOVED FOR SECURITY - Use Secret instead\n  # $1'
    Write-Host "   ✓ DB_DSN removed from ConfigMap" -ForegroundColor Green
}

if (-not $DryRun) {
    Set-Content $configMapFile -Value $configMapContent
    Write-Host "   ✓ ConfigMap saved" -ForegroundColor Green
}

# Step 3: Update StatefulSet
Write-Host ""
Write-Host "3. Updating StatefulSet..." -ForegroundColor Yellow

$statefulSetContent = Get-Content $statefulSetFile -Raw

# Update image reference
$oldImage = 'image: cybermesh/consensus-backend:latest'
$newImage = "image: gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag"
$statefulSetContent = $statefulSetContent -replace [regex]::Escape($oldImage), $newImage
Write-Host "   ✓ Image: cybermesh/consensus-backend:latest → gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag" -ForegroundColor Green

# Update imagePullPolicy for production
$statefulSetContent = $statefulSetContent -replace 'imagePullPolicy: IfNotPresent', 'imagePullPolicy: Always'
Write-Host "   ✓ imagePullPolicy: IfNotPresent → Always" -ForegroundColor Green

# Update storageClassName to premium-rwo (SSD)
$statefulSetContent = $statefulSetContent -replace 'storageClassName: "standard-rwo"', 'storageClassName: "premium-rwo"'
Write-Host "   ✓ storageClassName: standard-rwo → premium-rwo (SSD)" -ForegroundColor Green

if (-not $DryRun) {
    Set-Content $statefulSetFile -Value $statefulSetContent
    Write-Host "   ✓ StatefulSet saved" -ForegroundColor Green
}

# Step 4: Generate GKE deployment commands
Write-Host ""
Write-Host "4. Generating deployment commands..." -ForegroundColor Yellow

$deployScript = @"
#!/bin/bash
# GKE Deployment Commands for CyberMesh
# Generated: $timestamp

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== CyberMesh GKE Deployment ==="
echo ""

# Check prerequisites
echo -e "\${YELLOW}Checking prerequisites...\${NC}"

if ! command -v gcloud &> /dev/null; then
    echo -e "\${RED}ERROR: gcloud CLI not found\${NC}"
    exit 1
fi

if ! command -v kubectl &> /dev/null; then
    echo -e "\${RED}ERROR: kubectl not found\${NC}"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo -e "\${RED}ERROR: docker not found\${NC}"
    exit 1
fi

echo -e "\${GREEN}✓ All prerequisites met\${NC}"
echo ""

# Push Docker image to GCR
echo -e "\${YELLOW}Pushing Docker image to GCR...\${NC}"

# Configure Docker for GCR
gcloud auth configure-docker --quiet

# Tag image
docker tag cybermesh/consensus-backend:latest \\
  gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag

# Push to GCR
docker push gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag

echo -e "\${GREEN}✓ Image pushed to GCR\${NC}"
echo ""

# Set kubectl context
echo -e "\${YELLOW}Setting kubectl context...\${NC}"
# UNCOMMENT AND MODIFY:
# gcloud container clusters get-credentials CLUSTER_NAME --region=REGION

echo ""
echo -e "\${YELLOW}Deploying to Kubernetes...\${NC}"

# Apply manifests in order
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/secret.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/configmap-db-cert.yaml
kubectl apply -f k8s/rbac.yaml
kubectl apply -f k8s/service-headless.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/pdb.yaml
kubectl apply -f k8s/statefulset.yaml

echo ""
echo -e "\${GREEN}✓ All manifests applied\${NC}"
echo ""

# Wait for rollout
echo -e "\${YELLOW}Waiting for StatefulSet rollout...\${NC}"
kubectl rollout status statefulset/validator -n cybermesh --timeout=5m

echo ""
echo -e "\${GREEN}✓ Deployment complete!\${NC}"
echo ""

# Show status
echo -e "\${YELLOW}Deployment status:\${NC}"
kubectl get all -n cybermesh

echo ""
echo -e "\${YELLOW}Pod logs (validator-0):\${NC}"
kubectl logs -n cybermesh validator-0 --tail=20

echo ""
echo -e "\${GREEN}=== Deployment Complete ===${NC}"
echo ""
echo "Next steps:"
echo "  1. Verify all 5 validators are Running"
echo "  2. Check API health: kubectl exec -n cybermesh validator-0 -- curl http://localhost:9441/api/v1/health"
echo "  3. Monitor logs: kubectl logs -n cybermesh -l component=validator -f"
echo "  4. Get LoadBalancer IP: kubectl get svc validator-api -n cybermesh"
echo ""
"@

$deployScriptPath = Join-Path $k8sDir "deploy_to_gke.sh"
Set-Content $deployScriptPath -Value $deployScript
Write-Host "   ✓ Deployment script created: deploy_to_gke.sh" -ForegroundColor Green

# Step 5: Summary
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "Changes made:" -ForegroundColor Yellow
Write-Host "  ✓ ConfigMap: ENVIRONMENT → production" -ForegroundColor Green
Write-Host "  ✓ ConfigMap: API_TLS_ENABLED → true" -ForegroundColor Green
Write-Host "  ✓ ConfigMap: DB_DSN commented out (security)" -ForegroundColor Green
Write-Host "  ✓ StatefulSet: Image → gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag" -ForegroundColor Green
Write-Host "  ✓ StatefulSet: imagePullPolicy → Always" -ForegroundColor Green
Write-Host "  ✓ StatefulSet: storageClassName → premium-rwo" -ForegroundColor Green
Write-Host ""

Write-Host "Files created:" -ForegroundColor Yellow
Write-Host "  ✓ Backups in: $backupDir" -ForegroundColor Green
Write-Host "  ✓ Deployment script: deploy_to_gke.sh" -ForegroundColor Green
Write-Host ""

Write-Host "⚠ IMPORTANT: Before deploying to GKE:" -ForegroundColor Red
Write-Host "  1. Rotate all secrets in k8s/secret.yaml" -ForegroundColor Yellow
Write-Host "  2. Create TLS certificates for API server" -ForegroundColor Yellow
Write-Host "  3. Review resource limits in statefulset.yaml" -ForegroundColor Yellow
Write-Host "  4. Create GKE cluster (see GKE_READINESS_CHECKLIST.md)" -ForegroundColor Yellow
Write-Host "  5. Push Docker image: docker push gcr.io/$GCPProjectID/cybermesh-consensus:$ImageTag" -ForegroundColor Yellow
Write-Host ""

Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "  1. Review changes in configmap.yaml and statefulset.yaml" -ForegroundColor Cyan
Write-Host "  2. Update secrets (CRITICAL!)" -ForegroundColor Cyan
Write-Host "  3. Run: chmod +x k8s/deploy_to_gke.sh" -ForegroundColor Cyan
Write-Host "  4. Run: ./k8s/deploy_to_gke.sh" -ForegroundColor Cyan
Write-Host ""

if ($DryRun) {
    Write-Host "⚠ DRY RUN MODE - No files were modified" -ForegroundColor Yellow
    Write-Host "  Run without -DryRun to apply changes" -ForegroundColor Yellow
}

Write-Host "=== Done ===" -ForegroundColor Green
