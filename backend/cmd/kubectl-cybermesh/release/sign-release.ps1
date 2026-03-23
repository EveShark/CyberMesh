param(
    [string]$ArtifactsDir = ".\bin",
    [string]$PrivateKey = ".\release-private-key.pem",
    [string]$PublicKey = ".\release-public-key.pem"
)

$ErrorActionPreference = "Stop"
go run ./cmd/kubectl-cybermesh/release/sigtool --mode sign --artifacts-dir $ArtifactsDir --private-key $PrivateKey --write-public-key $PublicKey
