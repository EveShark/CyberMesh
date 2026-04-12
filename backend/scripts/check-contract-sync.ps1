$ErrorActionPreference = "Stop"

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
$sourceDir = Join-Path $repoRoot "security\contracts"
$targetDir = Join-Path $repoRoot "backend\pkg\security\contracts"

$sourceFiles = Get-ChildItem $sourceDir -Filter *.go | Sort-Object Name
$targetFiles = Get-ChildItem $targetDir -Filter *.go | Sort-Object Name

if ($sourceFiles.Count -ne $targetFiles.Count) {
    Write-Error "contract file count mismatch between security/contracts and backend/pkg/security/contracts"
}

foreach ($sourceFile in $sourceFiles) {
    $targetFile = Join-Path $targetDir $sourceFile.Name
    if (-not (Test-Path $targetFile)) {
        Write-Error "missing mirrored contract file: $($sourceFile.Name)"
    }

    $sourceContent = (Get-Content $sourceFile.FullName -Raw).Replace("`r`n", "`n")
    $targetContent = (Get-Content $targetFile -Raw).Replace("`r`n", "`n")
    if ($sourceContent -ne $targetContent) {
        Write-Error "contract drift detected for $($sourceFile.Name)"
    }
}

Write-Host "backend contract sync check passed"
