param(
    [string]$ArtifactsDir = ""
)

$ErrorActionPreference = "Stop"

if (-not $ArtifactsDir) {
    $scriptRoot = if ($PSScriptRoot) {
        $PSScriptRoot
    } elseif ($PSCommandPath) {
        Split-Path -Parent $PSCommandPath
    } else {
        Split-Path -Parent $MyInvocation.MyCommand.Path
    }
    $ArtifactsDir = Join-Path (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $scriptRoot))) "bin"
}

$ArtifactsDir = [System.IO.Path]::GetFullPath($ArtifactsDir)
$artifactNames = @(
    "kubectl-cybermesh.exe",
    "kubectl-cybermesh",
    "kubectl-cybermesh.bash",
    "_kubectl-cybermesh",
    "kubectl-cybermesh.ps1",
    "install-kubectl-cybermesh.ps1",
    "uninstall-kubectl-cybermesh.ps1",
    "install-kubectl-cybermesh.sh",
    "uninstall-kubectl-cybermesh.sh"
)

$lines = foreach ($name in $artifactNames) {
    $path = Join-Path $ArtifactsDir $name
    if (-not (Test-Path -LiteralPath $path)) {
        throw "Missing artifact: $path"
    }
    if ((Get-Item -LiteralPath $path).Length -eq 0) {
        throw "Artifact is empty: $path"
    }
    $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    "$hash  $name"
}

$outputPath = Join-Path $ArtifactsDir "SHA256SUMS"
Set-Content -LiteralPath $outputPath -Value $lines -Encoding ascii
Write-Output $outputPath
