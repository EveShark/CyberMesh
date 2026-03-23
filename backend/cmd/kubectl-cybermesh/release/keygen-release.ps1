param(
    [string]$PrivateKey = ".\release-private-key.pem",
    [string]$PublicKey = ".\release-public-key.pem"
)

$ErrorActionPreference = "Stop"
go run ./cmd/kubectl-cybermesh/release/sigtool --mode keygen --private-key $PrivateKey --public-key $PublicKey
