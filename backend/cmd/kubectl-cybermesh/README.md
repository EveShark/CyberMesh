# kubectl-cybermesh

`kubectl-cybermesh` is a standalone operator CLI for the CyberMesh backend API.

It is not deployed as a pod, sidecar, or cluster service. Operators install the
binary locally or on a bastion host and it talks to the existing backend REST
API.

## Build

Windows:

```powershell
cd B:\CyberMesh\backend
go build -o bin/kubectl-cybermesh.exe ./cmd/kubectl-cybermesh
```

Linux:

```powershell
cd B:\CyberMesh\backend
$env:CGO_ENABLED=0
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -o bin/kubectl-cybermesh ./cmd/kubectl-cybermesh
```

Or use:

```powershell
cd B:\CyberMesh\backend
make build-plugin
```

## Install

The recommended model is:

- install the binary into a stable directory on `PATH`
- rerun the same installer to upgrade in place
- optionally install shell completion alongside it

For `kubectl` plugin discovery, the binary name must stay `kubectl-cybermesh`
or `kubectl-cybermesh.exe`.

### Windows

From [backend](/B:/CyberMesh/backend):

```powershell
cd B:\CyberMesh\backend
.\cmd\kubectl-cybermesh\install\install.ps1
```

With PowerShell completion:

```powershell
cd B:\CyberMesh\backend
.\cmd\kubectl-cybermesh\install\install.ps1 -InstallCompletion
```

The installer:

- copies `kubectl-cybermesh.exe` into `%USERPROFILE%\bin` by default
- verifies `kubectl-cybermesh.exe` against `bin/SHA256SUMS` before install
- can also verify `SHA256SUMS.sig` when you pass a trusted public key with `-PublicKeyPath`
- validates the staged binary before replacing an existing install
- only adds that directory to the user `PATH` when you explicitly pass `-UpdatePath`
- can be rerun later to upgrade the binary in place

To install into a different directory:

```powershell
.\cmd\kubectl-cybermesh\install\install.ps1 -InstallDir C:\Tools\CyberMesh
```

To also update the user PATH explicitly:

```powershell
.\cmd\kubectl-cybermesh\install\install.ps1 -UpdatePath
```

To enforce signature verification before install:

```powershell
.\cmd\kubectl-cybermesh\install\install.ps1 -PublicKeyPath .\release-public-key.pem -RequireSignature
```

Signature verification currently requires Python plus the `cryptography`
package on the install host.

### Linux

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/install.sh
```

With bash completion:

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/install.sh --install-completion
```

With an explicit, managed PATH profile update:

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/install.sh --update-shell-profile
```

With explicit signature verification:

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/install.sh --public-key-path ./release-public-key.pem --require-signature
```

Signature verification currently requires Python plus the `cryptography`
package on the install host.

The installer:

- copies `kubectl-cybermesh` into `~/.local/bin` by default
- verifies `kubectl-cybermesh` against `bin/SHA256SUMS` before install
- can also verify `SHA256SUMS.sig` when you pass a trusted public key with `--public-key-path`
- validates the staged binary before replacing an existing install
- can be rerun later to upgrade the binary in place
- writes bash completion into `~/.local/share/bash-completion/completions` when requested
- only updates your shell profile if you explicitly pass `--update-shell-profile`
- writes a clearly marked managed PATH block instead of editing arbitrary lines

After install, open a new shell and run:

```powershell
kubectl-cybermesh version
kubectl cybermesh version
```

## Update

Updates use the same installer as first-time install.

Windows:

```powershell
cd B:\CyberMesh\backend
.\cmd\kubectl-cybermesh\install\install.ps1
```

Linux:

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/install.sh
```

The installer is idempotent:

- first run: install
- later run: upgrade in place
- existing `PATH` entry is preserved
- if `--update-shell-profile` is used, the managed PATH block is not duplicated
- a failed staged upgrade restores the previous working binary

## Uninstall

Windows:

```powershell
cd B:\CyberMesh\backend
.\cmd\kubectl-cybermesh\install\uninstall.ps1
```

Linux:

```sh
cd /path/to/CyberMesh/backend
sh ./cmd/kubectl-cybermesh/install/uninstall.sh
```

If you installed into a custom directory, pass that same directory to the
uninstall script.

If the installer added PATH state, uninstall removes only the installer-managed
PATH state. If you used `--update-shell-profile`, uninstall removes only the
managed `kubectl-cybermesh` PATH block.

## Checksums

Generate release checksums from [backend](/B:/CyberMesh/backend):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\cmd\kubectl-cybermesh\release\generate-checksums.ps1 -ArtifactsDir .\bin
```

Or on Linux:

```sh
sh ./cmd/kubectl-cybermesh/release/generate-checksums.sh ./bin
```

This writes:

- `bin/SHA256SUMS`

Verify on Windows:

```powershell
(Get-FileHash .\bin\kubectl-cybermesh.exe -Algorithm SHA256).Hash
Get-Content .\bin\SHA256SUMS
```

Verify on Linux:

```sh
sha256sum -c SHA256SUMS
```

Checksums should be generated from the final release artifacts only.

## Signing

The release flow supports Ed25519 signing of `SHA256SUMS`.

Generate a signing keypair:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\cmd\kubectl-cybermesh\release\keygen-release.ps1 -PrivateKey .\release-private-key.pem -PublicKey .\release-public-key.pem
```

Sign the release checksums:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\cmd\kubectl-cybermesh\release\sign-release.ps1 -ArtifactsDir .\bin -PrivateKey .\release-private-key.pem -PublicKey .\release-public-key.pem
```

Verify the release signature:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\cmd\kubectl-cybermesh\release\verify-release.ps1 -ArtifactsDir .\bin -PublicKey .\release-public-key.pem
```

Linux equivalents:

```sh
sh ./cmd/kubectl-cybermesh/release/keygen-release.sh ./release-private-key.pem ./release-public-key.pem
sh ./cmd/kubectl-cybermesh/release/sign-release.sh ./bin ./release-private-key.pem ./release-public-key.pem
sh ./cmd/kubectl-cybermesh/release/verify-release.sh ./bin ./release-public-key.pem
```

This writes:

- `bin/SHA256SUMS.sig`
- `release-public-key.pem` when signing with the wrapper scripts

Recommended release order:

1. build final artifacts
2. generate `SHA256SUMS`
3. sign `SHA256SUMS`
4. verify signature before publishing

Installers can also verify the signature at install time if you provide a
trusted public key out of band. Do not trust a public key copied from the same
untrusted artifact bundle you are trying to verify.

## Defaults

The plugin defaults to the public backend endpoint:

```text
https://api.cybermesh.qzz.io
```

Override precedence:

1. `--base-url`
2. `CYBERMESH_BASE_URL`
3. built-in default

Tenant precedence:

1. `--tenant`
2. `CYBERMESH_TENANT`

Auth precedence:

1. `--token`
2. `--api-key`
3. `CYBERMESH_TOKEN`
4. `CYBERMESH_API_KEY`

Interactive precedence:

1. `--interactive`
2. `--no-interactive`
3. terminal auto-detection

Interactive mode is now the default for supported read commands when stdout is a
real terminal and output is `table`. JSON output and redirected output stay
non-interactive.

Interactive keys:

- `j/k` or arrow keys: move selection
- `?`: open in-screen help
- `Enter`: open/close expanded detail
- `o`: open linked context bundle
- `t`: toggle linked trace pane
- `u`: toggle recent audit pane on `policies` and `workflows`
- `PgUp` / `PgDn`: scroll the full detail body
- `Ctrl+U` / `Ctrl+D`: faster full-body scroll
- `q`: quit

## Environment Variables

- `CYBERMESH_BASE_URL`
- `CYBERMESH_TENANT`
- `CYBERMESH_TOKEN`
- `CYBERMESH_API_KEY`
- `CYBERMESH_OUTPUT`
- `CYBERMESH_MTLS_CERT_FILE`
- `CYBERMESH_MTLS_KEY_FILE`
- `CYBERMESH_CA_FILE`

## Examples

Read operations:

```powershell
kubectl cybermesh version
kubectl cybermesh policies list --limit 20
kubectl cybermesh policies get <policy_id>
kubectl cybermesh policies coverage <policy_id>
kubectl cybermesh workflows list --limit 20
kubectl cybermesh workflows get <workflow_id>
kubectl cybermesh audit get --workflow-id incident-safe --limit 20
kubectl cybermesh audit export --workflow-id incident-safe --limit 100
kubectl cybermesh control
kubectl cybermesh validators
kubectl cybermesh consensus
kubectl cybermesh trace policy <policy_id>
kubectl cybermesh trace policy <policy_id> --interactive
kubectl cybermesh trace workflow incident-safe --interactive
kubectl cybermesh monitor --workflow-id incident-safe
kubectl cybermesh outbox get --policy-id <policy_id>
kubectl cybermesh ack get --policy-id <policy_id>
```

Tenant-scoped mutation:

```powershell
kubectl cybermesh --tenant default revoke --outbox-id <outbox_id> --reason-code operator.revoke --reason-text "manual revoke" --yes
kubectl cybermesh --tenant default policies revoke <policy_id> --reason-code operator.revoke --reason-text "manual revoke" --yes
kubectl cybermesh --tenant default policies approve <policy_id> --reason-code operator.approve --reason-text "manual approve" --yes
kubectl cybermesh --tenant default policies reject <policy_id> --reason-code operator.reject --reason-text "manual reject" --yes
kubectl cybermesh --tenant default workflows rollback <workflow_id> --reason-code operator.rollback --reason-text "bounded rollback" --yes
kubectl cybermesh kill-switch enable --reason-code operator.freeze --reason-text "maintenance freeze" --yes
kubectl cybermesh kill-switch disable --reason-code operator.freeze --reason-text "maintenance done" --yes
```

JSON output:

```powershell
kubectl cybermesh -o json outbox get --workflow-id incident-42
```

Shell completions:

```powershell
kubectl cybermesh completion powershell > kubectl-cybermesh.ps1
kubectl cybermesh completion bash > kubectl-cybermesh.bash
kubectl cybermesh completion zsh > _kubectl-cybermesh
```

Release helpers:

```powershell
cd B:\CyberMesh\backend
make build-plugin-win
make build-plugin-linux
make plugin-completions
make plugin-installers
make plugin-checksums
make release-plugin
```

Generated installer artifacts:

- `bin/install-kubectl-cybermesh.ps1`
- `bin/uninstall-kubectl-cybermesh.ps1`
- `bin/install-kubectl-cybermesh.sh`
- `bin/uninstall-kubectl-cybermesh.sh`
- `bin/SHA256SUMS`

## Notes

- Mutations may require `--tenant` depending on backend policy.
- `safe-mode` is intended as a cluster-wide mutation gate.
- `kill-switch` is a stronger cluster-wide mutation gate and should be used
  carefully.
- The plugin uses the current backend API surface; it does not require a
  separate plugin service.
- `policies list|get`, `workflows list|get`, and `audit get` are interactive by
  default on a real terminal.
- `control` is also interactive by default on a real terminal.
- `--interactive` forces interactive mode.
- `--no-interactive` forces plain table mode.
- `--interactive` also enables the optional terminal UI for supported `trace`
  commands.
- `monitor` opens the interactive consensus/outbox/ack view.
- interactive destructive actions in `policies`, `workflows`, and `control`
  open an editable draft for reason code, reason text, and classification
  before submit.
- expanded linked panes can show trace and recent audit context without leaving
  the current screen.
- `CYBERMESH_TUI_AUTOEXIT=1` is kept as a hidden smoke-test hook for validation
  and CI only. It is not part of the normal operator UX.
