param(
    [string]$SourcePath = "",
    [string]$InstallDir = "",
    [switch]$InstallCompletion,
    [switch]$UpdatePath = $false,
    [string]$PublicKeyPath = "",
    [string]$SignaturePath = "",
    [switch]$RequireSignature = $false
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

function Resolve-SourceBinary {
    param([string]$RequestedPath)

    if ($RequestedPath -and (Test-Path -LiteralPath $RequestedPath)) {
        return (Resolve-Path -LiteralPath $RequestedPath).Path
    }

    $scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
    $repoBinary = Join-Path (Split-Path -Parent (Split-Path -Parent $scriptRoot)) "bin\kubectl-cybermesh.exe"
    if (Test-Path -LiteralPath $repoBinary) {
        return (Resolve-Path -LiteralPath $repoBinary).Path
    }

    throw "Could not find kubectl-cybermesh.exe. Pass -SourcePath explicitly."
}

function Get-ChecksumFile {
    param([string]$BinaryPath)
    $dir = Split-Path -Parent $BinaryPath
    return (Join-Path $dir "SHA256SUMS")
}

function Get-SignatureFile {
    param([string]$ChecksumFile)
    return "$ChecksumFile.sig"
}

function Resolve-Python {
    foreach ($candidate in @("python", "py")) {
        $cmd = Get-Command $candidate -ErrorAction SilentlyContinue
        if ($cmd) {
            return $cmd.Source
        }
    }
    return $null
}

function Verify-Signature {
    param(
        [string]$ChecksumFile,
        [string]$ResolvedPublicKey,
        [string]$ResolvedSignature
    )

    if (-not (Test-Path -LiteralPath $ResolvedPublicKey)) {
        throw "Missing public key file: $ResolvedPublicKey"
    }
    if (-not (Test-Path -LiteralPath $ResolvedSignature)) {
        throw "Missing signature file: $ResolvedSignature"
    }

    $python = Resolve-Python
    if (-not $python) {
        throw "Signature verification requires python or py on PATH"
    }

    $script = @'
import base64
import pathlib
import sys
try:
    from cryptography.exceptions import InvalidSignature
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
except ImportError as exc:
    raise SystemExit("python cryptography package is required for signature verification") from exc

checksums_path = pathlib.Path(sys.argv[1])
public_key_path = pathlib.Path(sys.argv[2])
signature_path = pathlib.Path(sys.argv[3])

public_key = serialization.load_pem_public_key(public_key_path.read_bytes())
if not isinstance(public_key, Ed25519PublicKey):
    raise SystemExit("public key is not Ed25519")

payload = checksums_path.read_bytes()
signature = base64.b64decode(signature_path.read_text(encoding="utf-8").strip())
try:
    public_key.verify(signature, payload)
except InvalidSignature as exc:
    raise SystemExit("signature verification failed") from exc
'@

    $scriptPath = Join-Path ([System.IO.Path]::GetDirectoryName($ChecksumFile)) ".kubectl-cybermesh-verify-signature.py"
    try {
        Set-Content -LiteralPath $scriptPath -Value $script -Encoding UTF8
        & $python $scriptPath $ChecksumFile $ResolvedPublicKey $ResolvedSignature
        if ($LASTEXITCODE -ne 0) {
            throw "Signature verification failed"
        }
    }
    finally {
        if (Test-Path -LiteralPath $scriptPath) {
            Remove-Item -LiteralPath $scriptPath -Force
        }
    }
}

function Verify-Checksum {
    param(
        [string]$BinaryPath,
        [string]$ChecksumFile
    )

    if (-not (Test-Path -LiteralPath $ChecksumFile)) {
        throw "Missing checksum file: $ChecksumFile"
    }
    $fileName = Split-Path -Leaf $BinaryPath
    $line = Get-Content -LiteralPath $ChecksumFile | Where-Object {
        $parts = ($_ -split '\s+', 2)
        $parts.Count -eq 2 -and $parts[1].Trim() -eq $fileName
    } | Select-Object -First 1
    if (-not $line) {
        throw "No checksum entry for $fileName in $ChecksumFile"
    }
    $expected = (($line -split '\s+', 2)[0]).Trim().ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $BinaryPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        throw "Checksum mismatch for $fileName"
    }
}

function Ensure-PathEntry {
    param([string]$Dir)

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @()
    if ($userPath) {
        $entries = $userPath.Split(';') | Where-Object { $_ -ne "" }
    }
    if ($entries -contains $Dir) {
        Write-Step "PATH already contains $Dir"
        return
    }

    $newPath = (($entries + $Dir) -join ';').Trim(';')
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = (($env:Path.TrimEnd(';')) + ';' + $Dir).Trim(';')
    Write-Step "Added $Dir to the user PATH"
}

function Install-CompletionScript {
    param(
        [string]$InstallRoot,
        [string]$BinaryPath
    )

    $completionPath = Join-Path $InstallRoot "kubectl-cybermesh.ps1"
    $completion = & $BinaryPath completion powershell
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to generate PowerShell completion"
    }
    Set-Content -LiteralPath $completionPath -Value $completion -Encoding UTF8
    Write-Step "Wrote PowerShell completion to $completionPath"
    return $completionPath
}

function Write-InstallManifest {
    param(
        [string]$ManifestPath,
        [string]$InstallRoot,
        [bool]$PathUpdated,
        [bool]$CompletionInstalled,
        [string]$CompletionPath
    )

    $payload = @{
        install_dir = $InstallRoot
        path_updated = $PathUpdated
        completion_installed = $CompletionInstalled
        completion_path = $CompletionPath
    }
    $payload | ConvertTo-Json | Set-Content -LiteralPath $ManifestPath -Encoding UTF8
}

$sourceBinary = Resolve-SourceBinary -RequestedPath $SourcePath
$checksumFile = Get-ChecksumFile -BinaryPath $sourceBinary
$signatureFile = if ($SignaturePath) { [System.IO.Path]::GetFullPath($SignaturePath) } else { Get-SignatureFile -ChecksumFile $checksumFile }
if ($PublicKeyPath) {
    $PublicKeyPath = [System.IO.Path]::GetFullPath($PublicKeyPath)
}
if ($RequireSignature -and -not $PublicKeyPath) {
    throw "RequireSignature needs -PublicKeyPath."
}
if ($PublicKeyPath) {
    Verify-Signature -ChecksumFile $checksumFile -ResolvedPublicKey $PublicKeyPath -ResolvedSignature $signatureFile
} elseif ($RequireSignature) {
    throw "Signature verification was required but no public key was provided."
}
Verify-Checksum -BinaryPath $sourceBinary -ChecksumFile $checksumFile

if (-not $InstallDir) {
    $InstallDir = Get-DefaultInstallDir
}

$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$targetBinary = Join-Path $InstallDir "kubectl-cybermesh.exe"
$manifestPath = Join-Path $InstallDir $ManifestName
$tempBinary = Join-Path $InstallDir ".kubectl-cybermesh.tmp.exe"
$backupBinary = Join-Path $InstallDir ".kubectl-cybermesh.backup.exe"

$previousVersion = $null
if (Test-Path -LiteralPath $targetBinary) {
    try {
        $previousVersion = & $targetBinary version 2>$null
    } catch {
        $previousVersion = $null
    }
    Copy-Item -LiteralPath $targetBinary -Destination $backupBinary -Force
}

try {
    Copy-Item -LiteralPath $sourceBinary -Destination $tempBinary -Force
    try {
        $tempVersion = & $tempBinary version 2>$null
    } catch {
        throw "Installed binary failed validation: $($_.Exception.Message)"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "Installed binary failed validation: version command exited with code $LASTEXITCODE"
    }
    if (-not $tempVersion) {
        throw "Installed binary failed validation: version command returned no output"
    }

    Move-Item -LiteralPath $tempBinary -Destination $targetBinary -Force
    Write-Step "Installed kubectl-cybermesh.exe to $targetBinary"

    $completionPath = ""
    if ($UpdatePath) {
        Ensure-PathEntry -Dir $InstallDir
    }

    if ($InstallCompletion) {
        $completionPath = Install-CompletionScript -InstallRoot $InstallDir -BinaryPath $targetBinary
    }

    try {
        $currentVersion = & $targetBinary version 2>$null
    } catch {
        throw "Installed binary failed post-install validation: $($_.Exception.Message)"
    }
    if ($LASTEXITCODE -ne 0) {
        throw "Installed binary failed post-install validation: version command exited with code $LASTEXITCODE"
    }
    if (-not $currentVersion) {
        throw "Installed binary failed post-install validation"
    }

    Write-InstallManifest -ManifestPath $manifestPath -InstallRoot $InstallDir -PathUpdated:$UpdatePath -CompletionInstalled:$InstallCompletion -CompletionPath $completionPath

    if ($previousVersion) {
        Write-Step "Upgrade complete"
        Write-Step "Previous version: $previousVersion"
    } else {
        Write-Step "Install complete"
    }
    Write-Step "Current version: $currentVersion"
    if ($UpdatePath) {
        Write-Step "Open a new shell, then run: kubectl-cybermesh version"
    } else {
        Write-Step "Add $InstallDir to PATH or rerun with -UpdatePath, then run: kubectl-cybermesh version"
    }
}
catch {
    if (Test-Path -LiteralPath $backupBinary) {
        Move-Item -LiteralPath $backupBinary -Destination $targetBinary -Force
    }
    throw
}
finally {
    if (Test-Path -LiteralPath $tempBinary) {
        Remove-Item -LiteralPath $tempBinary -Force
    }
    if (Test-Path -LiteralPath $backupBinary) {
        Remove-Item -LiteralPath $backupBinary -Force
    }
}
