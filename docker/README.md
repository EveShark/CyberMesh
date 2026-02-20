# CyberMesh Docker

Central Docker build assets for CyberMesh services.

Use `docker/BUILD.md` as the build/run source of truth. This README is a map and quick-reference.

## Current Layout

```text
docker/
  .dockerignore
  BUILD.md
  README.md

  ai-service/
    Dockerfile

  backend/
    .dockerignore
    Dockerfile
    bin/cybermesh
    certs/           # local-only dev certs; ignored from git

  frontend/
    .dockerignore
    Dockerfile
    Dockerfile.vite
    nginx.conf
    README.md
    GKE_DEPLOYMENT_GUIDE.md

  sentinel/
    Dockerfile

  telemetry/
    README.md
    feature-transformer/
      Dockerfile
      requirements.txt
    go-component/
      Dockerfile

  scrits/
    ai-service/
      entrypoint.sh
    frontend/
      build-and-push.sh
      build-and-push.ps1
```

## Images

Core images:

- `cybermesh-backend:latest`
- `cybermesh-frontend:latest`
- `cybermesh-ai-service:latest`

Telemetry images:

- `telemetry-bridge:latest`
- `telemetry-stream-processor:latest`
- `telemetry-gateway-adapter:latest`
- `telemetry-baremetal-adapter:latest`
- `telemetry-cloudlogs-adapter:latest`
- `telemetry-feature-transformer:latest`

Optional service image:

- `sentinel:latest` (from `docker/sentinel/Dockerfile`)

## Registry Notes

This repo should not be tied to one provider.

- Any OCI-compatible registry works (Artifact Registry, GHCR, ECR, ACR, OCI Registry).
- `docker/BUILD.md` currently includes concrete GCP Artifact Registry commands as examples.
- Replace registry prefix only, keep image names stable.

Example prefixes:

- `asia-southeast1-docker.pkg.dev/<project>/<repo>/`
- `ghcr.io/<org>/`
- `<account>.dkr.ecr.<region>.amazonaws.com/`
- `<registry>.azurecr.io/`
- `<region>.ocir.io/<tenancy>/<repo>/`

## Build Context Rules

Run builds from repo root unless a Dockerfile explicitly says otherwise.

- Backend: `docker/backend/Dockerfile` expects backend artifacts from root context.
- Frontend: `docker/frontend/Dockerfile` and `docker/frontend/Dockerfile.vite` from root context.
- AI service: `docker/ai-service/Dockerfile` now copies entrypoint from:
  - `docker/scrits/ai-service/entrypoint.sh`
- Telemetry: use Dockerfiles under `docker/telemetry/` with build args from `BUILD.md`.

## Script Locations

Scripts are now centralized in:

- `docker/scrits/ai-service/entrypoint.sh`
- `docker/scrits/frontend/build-and-push.sh`
- `docker/scrits/frontend/build-and-push.ps1`

If older docs mention `docker/ai-service/entrypoint.sh` or `docker/frontend/build-and-push.*`, treat them as stale paths.

## Security And Hygiene

- `docker/backend/certs/` is local-only and ignored.
- Do not commit private keys or generated certs into docker paths.
- Prebuilt binaries under docker paths should be intentional and reproducible.

## What Was Missing In The Old README

- Missing service groups (`frontend`, `sentinel`, `telemetry`).
- Missing telemetry image list.
- Missing new script location under `docker/scrits/`.
- Over-focused on OCI-specific wording.
- Outdated structure section not matching current filesystem.
