$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$protoDir = $root
$outGo = Join-Path $root "gen\\go"
$outPy = Join-Path $root "gen\\python"

if (-not (Get-Command protoc -ErrorAction SilentlyContinue)) {
  throw "protoc not found in PATH"
}

$protos = @(
  "telemetry_flow_v1.proto",
  "telemetry_feature_v1.proto",
  "telemetry_dlq_v1.proto",
  "telemetry_deepflow_v1.proto",
  "telemetry_pcap_request_v1.proto",
  "telemetry_pcap_result_v1.proto"
)

protoc `
  --proto_path="$protoDir" `
  --go_out="$outGo" `
  --go_opt=paths=source_relative `
  --python_out="$outPy" `
  @($protos)

Write-Host "Generated Go + Python protobufs into gen/"
