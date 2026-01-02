#!/usr/bin/env pwsh
# Script to prepare K8s manifests for GKE deployment
# Updated for CyberMesh Migration Analysis

param(
    [Parameter(Mandatory=$false)]
    [string]$GCPProjectID = "sunny-vehicle-482107-p5",
    
    [Parameter(Mandatory=$false)]
    [string]$GCPRegion = "us-central1",
    
    [Parameter(Mandatory=$false)]
    [string]$ImageTag = "latest",
    
    [Parameter(Mandatory=$false)]
    [switch]$DryRun
)

Write-Host "=== CyberMesh GKE Preparation Script ===" -ForegroundColor Cyan
Write-Host "Project: $GCPProjectID"
Write-Host "Region:  $GCPRegion"
Write-Host "Tag:     $ImageTag"
Write-Host ""

$k8sDir = $PSScriptRoot
$backupDir = Join-Path $k8sDir "backups"
$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"

# Files to process
$deploymentFiles = @(
    "statefulset.yaml", 
    "daemonset.yaml", 
    "ai-service-deployment.yaml", 
    "frontend-deployment.yaml"
)

$rbacFiles = @(
    "rbac.yaml",
    "ai-service-rbac.yaml",
    "enforcement-agent-rbac.yaml"
)

# -------------------------------------------------------------------------
# Step 1: Backup
# -------------------------------------------------------------------------
if (-not (Test-Path $backupDir)) {
    New-Item -ItemType Directory -Path $backupDir | Out-Null
}

Write-Host "1. Creating backups..." -ForegroundColor Yellow
Get-ChildItem -Path $k8sDir -Filter "*.yaml" | ForEach-Object {
    Copy-Item $_.FullName (Join-Path $backupDir "$($_.BaseName)_${timestamp}$($_.Extension)")
}
Write-Host "   ✓ Backups created in $backupDir" -ForegroundColor Green

# -------------------------------------------------------------------------
# Step 2: Update ConfigMap
# -------------------------------------------------------------------------
Write-Host ""
Write-Host "2. Updating ConfigMaps..." -ForegroundColor Yellow
$configMapFile = Join-Path $k8sDir "configmap.yaml"
if (Test-Path $configMapFile) {
    $content = Get-Content $configMapFile -Raw
    
    # Environment updates
    $content = $content -replace 'ENVIRONMENT: "development"', 'ENVIRONMENT: "production"'
    $content = $content -replace 'API_TLS_ENABLED: "false"', 'API_TLS_ENABLED: "true"'
    $content = $content -replace 'REGION: "disk"', 'REGION: "gke"' # Fix legacy region if present
    
    # Security
    if ($content -match 'DB_DSN: "postgresql://') {
        $content = $content -replace '(DB_DSN: "postgresql://[^"]*")', '# REMOVED FOR SECURITY - Use Secret instead'
        Write-Host "   ✓ Cleaned DB_DSN from main ConfigMap" -ForegroundColor Green
    }
    
    if (-not $DryRun) { Set-Content $configMapFile -Value $content }
}

# -------------------------------------------------------------------------
# Step 3: Update Images & Registries
# -------------------------------------------------------------------------
Write-Host ""
Write-Host "3. Updating Container Images (Artifact Registry)..." -ForegroundColor Yellow

$repoHost = "$GCPRegion-docker.pkg.dev"
$repoPath = "$GCPProjectID/cybermesh-repo"

foreach ($file in $deploymentFiles) {
    $path = Join-Path $k8sDir $file
    if (Test-Path $path) {
        $content = Get-Content $path -Raw
        
        # Regex to replace any image reference
        # Targets: ghcr.io/..., gcr.io/..., or plain image names
        # Replaces with: REGION-docker.pkg.dev/PROJECT/REPO/IMAGE:TAG
        
        # Backend
        if ($content -match 'image: .*cybermesh-backend.*') {
            $content = $content -replace 'image: .*cybermesh-backend.*', "image: $repoHost/$repoPath/cybermesh-backend:$ImageTag"
            Write-Host "   ✓ $file : Updated cybermesh-backend" -ForegroundColor Gray
        }
        
        # AI Service
        if ($content -match 'image: .*cybermesh-ai-service.*') {
            $content = $content -replace 'image: .*cybermesh-ai-service.*', "image: $repoHost/$repoPath/cybermesh-ai-service:$ImageTag"
            Write-Host "   ✓ $file : Updated cybermesh-ai-service" -ForegroundColor Gray
        }
        
        # Enforcement Agent
        if ($content -match 'image: .*enforcement-agent.*') {
            $content = $content -replace 'image: .*enforcement-agent.*', "image: $repoHost/$repoPath/enforcement-agent:$ImageTag"
            Write-Host "   ✓ $file : Updated enforcement-agent" -ForegroundColor Gray
        }

        # Frontend
        if ($content -match 'image: .*cybermesh-frontend.*') {
            $content = $content -replace 'image: .*cybermesh-frontend.*', "image: $repoHost/$repoPath/cybermesh-frontend:$ImageTag"
            Write-Host "   ✓ $file : Updated cybermesh-frontend" -ForegroundColor Gray
        }

        # Common updates
        $content = $content -replace 'imagePullPolicy: IfNotPresent', 'imagePullPolicy: Always'
        
        # Clean up imagePullSecrets (using Workload Identity)
        if ($content -match 'imagePullSecrets:') {
            $content = $content -replace '(?ms)^\s+imagePullSecrets:\s*\r?\n\s+-\s+name: \S+', ''
            Write-Host "   ✓ $file : Removed imagePullSecrets (using Workload Identity)" -ForegroundColor Gray
        }

        if (-not $DryRun) { Set-Content $path -Value $content }
    }
}
Write-Host "   ✓ All images updated to $repoHost/$repoPath/..." -ForegroundColor Green

# -------------------------------------------------------------------------
# Step 4: Inject Workload Identity
# -------------------------------------------------------------------------
Write-Host ""
Write-Host "4. Injecting Workload Identity Annotations..." -ForegroundColor Yellow

foreach ($file in $rbacFiles) {
    $path = Join-Path $k8sDir $file
    if (Test-Path $path) {
        $content = Get-Content $path -Raw
        $gsaEmail = "cybermesh-sa@$GCPProjectID.iam.gserviceaccount.com" # Default GSA
        
        # Determine specific GSA based on file
        if ($file -like "*ai-service*") { $gsaEmail = "ai-service-sa@$GCPProjectID.iam.gserviceaccount.com" }
        if ($file -like "*enforcement*") { $gsaEmail = "enforcement-sa@$GCPProjectID.iam.gserviceaccount.com" }
        
        # Add annotation if missing
        if ($content -notmatch "iam.gke.io/gcp-service-account") {
            $annotation = "  annotations:`n    iam.gke.io/gcp-service-account: $gsaEmail"
            $content = $content -replace "metadata:`n  name: (\S+)", "metadata:`n  name: `$1`n$annotation"
            Write-Host "   ✓ $file : Injected Workload Identity ($gsaEmail)" -ForegroundColor Gray
            if (-not $DryRun) { Set-Content $path -Value $content }
        } else {
            Write-Host "   - $file : Workload Identity already present" -ForegroundColor DarkGray
        }
    }
}
Write-Host "   ✓ Workload Identity configuration complete" -ForegroundColor Green

# -------------------------------------------------------------------------
# Step 5: Generate Deploy Script
# -------------------------------------------------------------------------
Write-Host ""
Write-Host "5. Generating deployment script..." -ForegroundColor Yellow

$deployScript = @"
#!/bin/bash
# GKE Deployment Script for CyberMesh
# Auto-generated: $timestamp
# Project: $GCPProjectID | Region: $GCPRegion

set -e

echo "=== CyberMesh GKE Deployment ==="
echo "Project: $GCPProjectID"
echo "Region:  $GCPRegion"
echo ""

# 1. Apply Namespace & RBAC
echo "--- 1. Security & Access Control ---"
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/rbac.yaml
kubectl apply -f k8s/ai-service-rbac.yaml
kubectl apply -f k8s/enforcement-agent-rbac.yaml

# 2. Configs & Secrets
echo "--- 2. Configuration ---"
# Note: Manually create secrets if they don't exist!
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/configmap-db-cert.yaml
kubectl apply -f k8s/ai-service-configmap.yaml
kubectl apply -f k8s/ai-service-entrypoint-configmap.yaml
if [ -f "k8s/secret.yaml" ]; then kubectl apply -f k8s/secret.yaml; fi
if [ -f "k8s/backend-trusted-keys-secret.yaml" ]; then kubectl apply -f k8s/backend-trusted-keys-secret.yaml; fi

# 3. Storage
echo "--- 3. Storage ---"
kubectl apply -f k8s/ai-service-pvc.yaml
kubectl apply -f k8s/ai-oci-storage-pvc.yaml

# 4. Services
echo "--- 4. Services ---"
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/service-headless.yaml
kubectl apply -f k8s/ai-service-service.yaml
kubectl apply -f k8s/frontend-service.yaml

# 5. Workloads
echo "--- 5. Workloads ---"
kubectl apply -f k8s/pdb.yaml
kubectl apply -f k8s/postgres-statefulset.yaml
kubectl apply -f k8s/statefulset.yaml
kubectl apply -f k8s/ai-service-deployment.yaml
kubectl apply -f k8s/frontend-deployment.yaml
kubectl apply -f k8s/daemonset.yaml

# 6. Jobs
echo "--- 6. Jobs ---"
kubectl apply -f k8s/utils/migrate-db-job.yaml

echo ""
echo "=== Deployment Applied ==="
echo "Monitor status:"
echo "  kubectl get pods -n cybermesh -w"
"@

$deployScriptPath = Join-Path $k8sDir "deploy_to_gke.sh"
Set-Content $deployScriptPath -Value $deployScript
# Make executable (if on linux/mac, but this is PS script running on Windows likely)
# In git bash: chmod +x would work
Write-Host "   ✓ Created deploy_to_gke.sh" -ForegroundColor Green

Write-Host ""
Write-Host "=== Done ===" -ForegroundColor Green
Write-Host "Verify files in $k8sDir before running deploy_to_gke.sh"
