# ============================================================
# Build and Push CyberMesh Frontend Docker Image (PowerShell)
# ============================================================
# Usage: .\build-and-push.ps1 [-Tag "v1.0.0"] [-Push]
# Example: .\build-and-push.ps1 -Tag "v1.0.0" -Push
# ============================================================

param(
    [string]$Tag = "latest",
    [string]$Region = "us-central1",
    [string]$ProjectId = "sunny-vehicle-482107-p5",
    [string]$Repository = "cybermesh-repo",
    [switch]$Push = $false
)

$ImageName = "cybermesh-frontend"
$Registry = "${Region}-docker.pkg.dev/${ProjectId}/${Repository}"
$FullImage = "${Registry}/${ImageName}:${Tag}"

Write-Host "=== Building CyberMesh Frontend Docker Image ===" -ForegroundColor Green
Write-Host "Image: $FullImage" -ForegroundColor Yellow
Write-Host "Registry: GKE Artifact Registry ($Region)" -ForegroundColor Cyan
Write-Host "Repository: $Repository" -ForegroundColor Cyan
Write-Host ""

# Navigate to project root
Set-Location (Join-Path $PSScriptRoot "..\..") -ErrorAction Stop

# Load environment variables from .env
$envFile = "cybermesh-frontend\.env"
if (Test-Path $envFile) {
    Write-Host "Loading environment variables from .env" -ForegroundColor Green
    
    $envVars = @{}
    Get-Content $envFile | ForEach-Object {
        if ($_ -match '^([^#=]+)=(.*)$') {
            $key = $matches[1].Trim()
            $value = $matches[2].Trim()
            $envVars[$key] = $value
        }
    }
    
    $VITE_SUPABASE_PROJECT_ID = $envVars['VITE_SUPABASE_PROJECT_ID']
    $VITE_SUPABASE_URL = $envVars['VITE_SUPABASE_URL']
    $VITE_SUPABASE_PUBLISHABLE_KEY = $envVars['VITE_SUPABASE_PUBLISHABLE_KEY']
    $VITE_DEMO_MODE = $envVars['VITE_DEMO_MODE']
    
    if (-not $VITE_SUPABASE_PROJECT_ID) {
        Write-Host "Error: VITE_SUPABASE_PROJECT_ID not found in .env" -ForegroundColor Red
        exit 1
    }
} else {
    Write-Host "Error: .env file not found at $envFile" -ForegroundColor Red
    exit 1
}

# Build Docker image with build arguments
Write-Host "Building Docker image..." -ForegroundColor Green
Write-Host ""

docker build `
    -f docker/frontend/Dockerfile.vite `
    -t "$FullImage" `
    --build-arg "VITE_SUPABASE_PROJECT_ID=$VITE_SUPABASE_PROJECT_ID" `
    --build-arg "VITE_SUPABASE_URL=$VITE_SUPABASE_URL" `
    --build-arg "VITE_SUPABASE_PUBLISHABLE_KEY=$VITE_SUPABASE_PUBLISHABLE_KEY" `
    --build-arg "VITE_DEMO_MODE=$VITE_DEMO_MODE" `
    .

if ($LASTEXITCODE -ne 0) {
    Write-Host "Build failed!" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "✓ Build complete!" -ForegroundColor Green
Write-Host ""

# Tag as latest
if ($Tag -ne "latest") {
    Write-Host "Tagging as latest..." -ForegroundColor Green
    docker tag "$FullImage" "${Registry}/${ImageName}:latest"
}

# Get image size
$imageInfo = docker images $FullImage --format "{{.Size}}"

Write-Host "=== Summary ===" -ForegroundColor Green
Write-Host "Image: $FullImage"
Write-Host "Size: $imageInfo"
Write-Host ""

# Push to registry
if ($Push) {
    Write-Host "Pushing to registry..." -ForegroundColor Green
    docker push "$FullImage"
    
    if ($Tag -ne "latest") {
        docker push "${Registry}/${ImageName}:latest"
    }
    
    Write-Host ""
    Write-Host "✓ Push complete!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Image available at:" -ForegroundColor Green
    Write-Host "  $FullImage"
    
    if ($Tag -ne "latest") {
        Write-Host "  ${Registry}/${ImageName}:latest"
    }
} else {
    Write-Host "Skipping push (use -Push flag to push)" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "To run locally:" -ForegroundColor Green
Write-Host "  docker run -p 3000:3000 $FullImage"
Write-Host ""
Write-Host "To push later:" -ForegroundColor Green
Write-Host "  docker push $FullImage"
Write-Host ""
Write-Host "To deploy to GKE:" -ForegroundColor Green
Write-Host "  kubectl set image deployment/frontend frontend=$FullImage -n cybermesh"
