# Manifest Validation Script
# Checks that all required files exist for Genesis Persistence deployment

$ErrorActionPreference = "Stop"

Write-Host "Validating Genesis Persistence Manifests..." -ForegroundColor Cyan
Write-Host ""

$manifests = @(
    "k8s_gke/utils/migration-sql-configmap.yaml",
    "k8s_gke/utils/migrate-db-job.yaml",
    "k8s_gke/utils/verify-db-job.yaml",
    "k8s_gke/utils/clear-db-job.yaml",
    "k8s_gke/utils/delete-genesis-job.yaml",
    "k8s_gke/configmap.yaml",
    "k8s_gke/statefulset.yaml"
)

$failed = 0

foreach ($manifest in $manifests) {
    $name = Split-Path $manifest -Leaf
    
    if (Test-Path $manifest) {
        $size = (Get-Item $manifest).Length
        Write-Host "[OK] $name ($size bytes)" -ForegroundColor Green
    }
    else {
        Write-Host "[FAIL] $name - File not found" -ForegroundColor Red
        $failed++
    }
}

Write-Host ""
if ($failed -eq 0) {
    Write-Host "All manifests validated successfully!" -ForegroundColor Green
    exit 0
}
else {
    Write-Host "$failed manifest(s) missing" -ForegroundColor Red
    exit 1
}
