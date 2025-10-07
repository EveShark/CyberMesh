Param(
  [Parameter(Mandatory=$true)] [string]$SourceEnvPath,
  [Parameter(Mandatory=$true)] [string]$TargetEnvPath
)

function Read-EnvFile($path) {
  $map = @{}
  Get-Content -LiteralPath $path | ForEach-Object {
    $line = $_.Trim()
    if ($line -match '^[#;]') { return }
    if ($line -match '^(.*?)=(.*)$') {
      $k = $Matches[1].Trim()
      $v = $Matches[2]
      $map[$k] = $v
    }
  }
  return $map
}

if (-not (Test-Path -LiteralPath $SourceEnvPath)) { Write-Error "Source env not found: $SourceEnvPath"; exit 1 }
if (-not (Test-Path -LiteralPath $TargetEnvPath)) { Write-Error "Target env not found: $TargetEnvPath"; exit 1 }

$src = Read-EnvFile $SourceEnvPath
$content = Get-Content -LiteralPath $TargetEnvPath -Raw

# Extract secrets (do NOT print to console)
$dbUrl = $null
if ($src.ContainsKey('COCKROACH_CLOUD_URL')) { $dbUrl = $src['COCKROACH_CLOUD_URL'] }
elseif ($src.ContainsKey('DATABASE_URL')) { $dbUrl = $src['DATABASE_URL'] }

$kafkaBrokers = $null
if ($src.ContainsKey('KAFKA_BOOTSTRAP_SERVERS')) { $kafkaBrokers = $src['KAFKA_BOOTSTRAP_SERVERS'] }

$kafkaUser = $null; if ($src.ContainsKey('KAFKA_SASL_USERNAME')) { $kafkaUser = $src['KAFKA_SASL_USERNAME'] }
$kafkaPass = $null; if ($src.ContainsKey('KAFKA_SASL_PASSWORD')) { $kafkaPass = $src['KAFKA_SASL_PASSWORD'] }
$kafkaMech = $null; if ($src.ContainsKey('KAFKA_SASL_MECHANISM')) { $kafkaMech = $src['KAFKA_SASL_MECHANISM'] } else { $kafkaMech = 'PLAIN' }

# Update helpers
function Set-Or-Add($text, $key, $value) {
  $pattern = "(?m)^(" + [regex]::Escape($key) + ")=.*$"
  if ($text -match $pattern) { return ([regex]::Replace($text, $pattern, "$key=$value")) }
  else { return ($text.TrimEnd() + "`r`n$key=$value`r`n") }
}

# Apply DB settings
if ($dbUrl) {
  $content = Set-Or-Add $content 'DB_DSN' $dbUrl
  $content = Set-Or-Add $content 'DB_TLS' 'true'
}

# Apply Kafka settings
if ($kafkaBrokers) {
  $content = Set-Or-Add $content 'ENABLE_KAFKA' 'true'
  $content = Set-Or-Add $content 'KAFKA_BROKERS' $kafkaBrokers
  $content = Set-Or-Add $content 'KAFKA_TLS_ENABLED' 'true'
  if ($kafkaMech) { $content = Set-Or-Add $content 'KAFKA_SASL_MECHANISM' $kafkaMech }
  if ($kafkaUser) { $content = Set-Or-Add $content 'KAFKA_SASL_USERNAME' $kafkaUser }
  if ($kafkaPass) { $content = Set-Or-Add $content 'KAFKA_SASL_PASSWORD' $kafkaPass }
}

# Backup and write
Copy-Item -LiteralPath $TargetEnvPath -Destination ($TargetEnvPath + '.bak') -Force
Set-Content -LiteralPath $TargetEnvPath -Value $content -NoNewline

Write-Host "Merged DB and Kafka settings into $TargetEnvPath (backup: $TargetEnvPath.bak)" -ForegroundColor Green
