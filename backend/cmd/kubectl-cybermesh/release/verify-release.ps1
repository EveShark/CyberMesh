param(
    [string]$ArtifactsDir = ".\bin",
    [string]$PublicKey = ".\release-public-key.pem"
)

$ErrorActionPreference = "Stop"
go run ./cmd/kubectl-cybermesh/release/sigtool --mode verify --artifacts-dir $ArtifactsDir --public-key $PublicKey
