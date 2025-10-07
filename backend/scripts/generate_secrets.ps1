# Generate cryptographically secure secrets for .env file
$rng = [Security.Cryptography.RNGCryptoServiceProvider]::Create()

# Encryption key (32 bytes = 64 hex chars)
$encBytes = New-Object byte[] 32
$rng.GetBytes($encBytes)
$encKey = -join ($encBytes | ForEach-Object { $_.ToString('x2') })

# Secret key (48 bytes base64)
$secBytes = New-Object byte[] 48
$rng.GetBytes($secBytes)
$secKey = [Convert]::ToBase64String($secBytes)

# Salt (24 bytes base64)
$saltBytes = New-Object byte[] 24
$rng.GetBytes($saltBytes)
$salt = [Convert]::ToBase64String($saltBytes)

# JWT secret (48 bytes base64)
$jwtBytes = New-Object byte[] 48
$rng.GetBytes($jwtBytes)
$jwtSecret = [Convert]::ToBase64String($jwtBytes)

# Audit signing key (48 bytes base64)
$auditBytes = New-Object byte[] 48
$rng.GetBytes($auditBytes)
$auditKey = [Convert]::ToBase64String($auditBytes)

# AI service encryption key (32 bytes base64)
$aiBytes = New-Object byte[] 32
$rng.GetBytes($aiBytes)
$aiKey = [Convert]::ToBase64String($aiBytes)

# AI rate limit secret (32 bytes base64)
$aiRateBytes = New-Object byte[] 32
$rng.GetBytes($aiRateBytes)
$aiRateKey = [Convert]::ToBase64String($aiRateBytes)

Write-Output "ENCRYPTION_KEY=$encKey"
Write-Output "SECRET_KEY=$secKey"
Write-Output "SALT=$salt"
Write-Output "JWT_SECRET=$jwtSecret"
Write-Output "AUDIT_SIGNING_KEY=$auditKey"
Write-Output "AI_SERVICE_ENCRYPTION_KEY=$aiKey"
Write-Output "AI_RATE_LIMIT_SECRET=$aiRateKey"
