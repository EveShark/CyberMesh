$ErrorActionPreference = "Stop"

$goModPath = Join-Path $PSScriptRoot "..\go.mod"
$goModPath = [System.IO.Path]::GetFullPath($goModPath)
$content = Get-Content $goModPath -Raw

$badPatterns = @(
    'replace\s+\S+\s+=>\s+\.\.',
    'replace\s+\S+\s+=>\s+\.\./',
    'replace\s+\S+\s+=>\s+\.\.\\'
)

foreach ($pattern in $badPatterns) {
    if ($content -match $pattern) {
        Write-Error "backend/go.mod contains forbidden parent-directory replace directive"
    }
}

Write-Host "backend/go.mod guard passed"
