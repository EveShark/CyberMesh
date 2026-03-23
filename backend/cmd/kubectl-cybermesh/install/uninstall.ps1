param(
    [string]$InstallDir = "",
    [switch]$RemoveFromPath
)

$ErrorActionPreference = "Stop"
$ManifestName = ".kubectl-cybermesh-install.json"

function Write-Step {
    param([string]$Message)
    Write-Host "[kubectl-cybermesh] $Message"
}

function Get-DefaultInstallDir {
    if ($env:USERPROFILE) {
        return (Join-Path $env:USERPROFILE "bin")
    }
    return (Join-Path $HOME "bin")
}

function Remove-PathEntry {
    param([string]$Dir)

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if (-not $userPath) {
        return
    }
    $entries = $userPath.Split(';') | Where-Object { $_ -and $_ -ne $Dir }
    $newPath = ($entries -join ';').Trim(';')
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = (($env:Path.Split(';') | Where-Object { $_ -and $_ -ne $Dir }) -join ';').Trim(';')
    Write-Step "Removed $Dir from the user PATH"
}

if (-not $InstallDir) {
    $InstallDir = Get-DefaultInstallDir
}

$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$manifestPath = Join-Path $InstallDir $ManifestName
$binaryPath = Join-Path $InstallDir "kubectl-cybermesh.exe"
$completionPath = Join-Path $InstallDir "kubectl-cybermesh.ps1"

$manifest = $null
if (Test-Path -LiteralPath $manifestPath) {
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
}

if (Test-Path -LiteralPath $binaryPath) {
    Remove-Item -LiteralPath $binaryPath -Force
    Write-Step "Removed $binaryPath"
}

$manifestCompletion = $null
if ($manifest -and $manifest.completion_path) {
    $manifestCompletion = [string]$manifest.completion_path
}
if ($manifestCompletion -and (Test-Path -LiteralPath $manifestCompletion)) {
    Remove-Item -LiteralPath $manifestCompletion -Force
    Write-Step "Removed $manifestCompletion"
} elseif (Test-Path -LiteralPath $completionPath) {
    Remove-Item -LiteralPath $completionPath -Force
    Write-Step "Removed $completionPath"
}

$shouldRemovePath = $RemoveFromPath.IsPresent
if (-not $shouldRemovePath -and $manifest -and $manifest.path_updated -eq $true) {
    $shouldRemovePath = $true
}
if ($shouldRemovePath) {
    Remove-PathEntry -Dir $InstallDir
}

if (Test-Path -LiteralPath $manifestPath) {
    Remove-Item -LiteralPath $manifestPath -Force
}

Write-Step "Uninstall complete"
