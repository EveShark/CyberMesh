# CyberMesh Frontend - Clean Start Script
# Fixes 404 errors by clearing cache and restarting

Write-Host "==================================" -ForegroundColor Cyan
Write-Host "  CyberMesh Frontend Clean Start  " -ForegroundColor Cyan
Write-Host "==================================" -ForegroundColor Cyan
Write-Host ""

# Navigate to frontend directory
Set-Location $PSScriptRoot

# Stop any running processes
Write-Host "[1/4] Checking for running processes on port 3000..." -ForegroundColor Yellow
$process = Get-NetTCPConnection -LocalPort 3000 -ErrorAction SilentlyContinue
if ($process) {
    Write-Host "  ⚠️  Port 3000 is in use. Please stop the dev server (Ctrl+C) first." -ForegroundColor Red
    Write-Host "  Or kill the process: taskkill /PID $($process.OwningProcess) /F" -ForegroundColor Gray
    exit 1
} else {
    Write-Host "  ✓ Port 3000 is available" -ForegroundColor Green
}

# Clean build cache
Write-Host "`n[2/4] Cleaning build cache..." -ForegroundColor Yellow
if (Test-Path ".next") {
    Remove-Item -Recurse -Force .next
    Write-Host "  ✓ Removed .next directory" -ForegroundColor Green
} else {
    Write-Host "  ✓ No build cache to clean" -ForegroundColor Green
}

# Verify files
Write-Host "`n[3/4] Verifying project files..." -ForegroundColor Yellow
$criticalFiles = @(
    "src\app\page.tsx",
    "app\overview.tsx",
    "src\components\node-status-grid.tsx",
    "src\lib\mode-context.tsx",
    "package.json"
)

$allExist = $true
foreach ($file in $criticalFiles) {
    if (Test-Path $file) {
        Write-Host "  ✓ $file" -ForegroundColor Green
    } else {
        Write-Host "  ✗ $file MISSING!" -ForegroundColor Red
        $allExist = $false
    }
}

if (-not $allExist) {
    Write-Host "`n  ⚠️  Some files are missing! Check installation." -ForegroundColor Red
    exit 1
}

# Check dependencies
if (-not (Test-Path "node_modules")) {
    Write-Host "`n  ⚠️  node_modules not found. Installing dependencies..." -ForegroundColor Yellow
    npm install
}

Write-Host "`n[4/4] Starting dev server..." -ForegroundColor Yellow
Write-Host "  Please wait 30-60 seconds for compilation..." -ForegroundColor Gray
Write-Host ""
Write-Host "==================================" -ForegroundColor Cyan
Write-Host "  Dashboard will open at:          " -ForegroundColor Cyan
Write-Host "  http://localhost:3000            " -ForegroundColor Green
Write-Host "==================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Press Ctrl+C to stop the server" -ForegroundColor Gray
Write-Host ""

# Start dev server
npm run dev
