# AKS Backend TLS

The validator API in AKS expects a TLS secret named `backend-tls` in namespace `cybermesh`.

Do not deploy the placeholder `localhost` certificate. It breaks:

- browser fetches to `https://api.cybermesh.qzz.io`
- Cloudflare proxy/origin SSL
- frontend pages that depend on `/api/v1/dashboard/overview`

## Required certificate

Use a certificate whose SAN includes:

- `api.cybermesh.qzz.io`

## Create or update the secret

```powershell
./scripts/create_backend_tls_secret.ps1 `
  -CertPath path\to\api.cybermesh.qzz.io.crt `
  -KeyPath path\to\api.cybermesh.qzz.io.key
```

Equivalent raw command:

```powershell
kubectl create secret tls backend-tls `
  --namespace cybermesh `
  --cert path\to\api.cybermesh.qzz.io.crt `
  --key path\to\api.cybermesh.qzz.io.key `
  --dry-run=client -o yaml | kubectl apply -f -
```

## Roll validators

```powershell
kubectl rollout restart statefulset/validator -n cybermesh
kubectl rollout status statefulset/validator -n cybermesh
```

## Verify

```powershell
kubectl get secret backend-tls -n cybermesh
curl.exe -I https://api.cybermesh.qzz.io/api/v1/health
curl.exe https://api.cybermesh.qzz.io/api/v1/dashboard/overview
```

If Cloudflare is proxying `api.cybermesh.qzz.io`, ensure its SSL mode matches the origin certificate strategy.
