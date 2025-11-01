# CyberMesh Unified Build System

## Overview

All Docker builds use a **unified structure** from the project root with consistent naming and registry.

**Registry:** `asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/`

**Single Source of Truth:** `/docker` directory

---

## Image Naming Convention

```
cybermesh-backend:latest
cybermesh-frontend:latest
cybermesh-ai-service:latest
```

Full paths:
```
asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest
asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest
asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest
```

---

## Prerequisites

### 1. Backend Binary (Required before building backend image)

```bash
cd B:\CyberMesh\backend
$env:CGO_ENABLED=0
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags="-s -w" -trimpath -o bin/cybermesh ./cmd/cybermesh/main.go
```

**Output:** `backend/bin/cybermesh` (35 MB Linux binary)

### 2. Google Cloud Authentication

```bash
gcloud auth configure-docker asia-southeast1-docker.pkg.dev
```

---

## Build Commands

**All builds run from project root** (`B:\CyberMesh`)

### Backend

```bash
cd B:\CyberMesh
docker build -f docker/backend/Dockerfile \
  -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest \
  .
```

**Context:** Project root  
**Dockerfile:** `docker/backend/Dockerfile`  
**Copies:** `backend/bin/cybermesh` → `/app/cybermesh`

### Frontend

```bash
cd B:\CyberMesh
docker build -f docker/frontend/Dockerfile \
  -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest \
  .
```

**Context:** Project root  
**Dockerfile:** `docker/frontend/Dockerfile`  
**Copies:** `frontend/` → `/app/`

### AI Service

```bash
cd B:\CyberMesh
docker build -f docker/ai-service/Dockerfile \
  -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest \
  .
```

**Context:** Project root  
**Dockerfile:** `docker/ai-service/Dockerfile`  
**Copies:** `ai-service/` → `/app/`

---

## Push Commands

### Backend

```bash
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest
```

### Frontend

```bash
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest
```

### AI Service

```bash
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest
```

---

## Complete Build & Push Workflow

### PowerShell (Windows)

```powershell
# Navigate to project root
cd B:\CyberMesh

# 1. Build backend binary
cd backend
$env:CGO_ENABLED=0; $env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags="-s -w" -trimpath -o bin/cybermesh ./cmd/cybermesh/main.go
cd ..

# 2. Build all Docker images
docker build -f docker/backend/Dockerfile -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest .
docker build -f docker/frontend/Dockerfile -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest .
docker build -f docker/ai-service/Dockerfile -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest .

# 3. Push all images
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-frontend:latest
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-ai-service:latest
```

---

## Kubernetes Deployment

All K8s manifests in `k8s_gke/` are already configured with correct image paths:

- **Backend:** `k8s_gke/statefulset.yaml`
- **Frontend:** `k8s_gke/frontend-deployment.yaml`
- **AI Service:** `k8s_gke/ai-service-deployment.yaml`

### Deploy/Update

```bash
# Backend
kubectl apply -f k8s_gke/statefulset.yaml -n cybermesh

# Frontend
kubectl apply -f k8s_gke/frontend-deployment.yaml -n cybermesh

# AI Service
kubectl apply -f k8s_gke/ai-service-deployment.yaml -n cybermesh
```

### Force Pull Latest Image

```bash
# Backend
kubectl rollout restart statefulset/validator -n cybermesh

# Frontend
kubectl rollout restart deployment/frontend -n cybermesh

# AI Service
kubectl rollout restart deployment/ai-service -n cybermesh
```

---

## Directory Structure

```
B:\CyberMesh/
├── docker/                          # Single source of truth for Docker builds
│   ├── .dockerignore               # Unified ignore rules for all builds
│   ├── BUILD.md                    # This file
│   ├── backend/
│   │   └── Dockerfile              # Backend build (uses backend/bin/cybermesh)
│   ├── frontend/
│   │   └── Dockerfile              # Frontend build (uses frontend/)
│   └── ai-service/
│       ├── Dockerfile              # AI service build (uses ai-service/)
│       └── entrypoint.sh           # AI service entrypoint
│
├── backend/
│   └── bin/
│       └── cybermesh               # Pre-built Linux binary (required)
│
├── frontend/                        # Next.js application
├── ai-service/                      # Python ML service
└── k8s_gke/                        # Kubernetes manifests
    ├── statefulset.yaml            # Backend deployment
    ├── frontend-deployment.yaml    # Frontend deployment
    └── ai-service-deployment.yaml  # AI service deployment
```

---

## Benefits

✅ **Single Registry** - All images in Artifact Registry  
✅ **Uniform Naming** - Consistent `cybermesh-*:latest` pattern  
✅ **No Tagging Confusion** - K8s manifests reference same tags as builds  
✅ **Clean Structure** - All Docker files in `/docker`  
✅ **Predictable Builds** - Same command pattern for all services  
✅ **No .dockerignore Hacks** - Unified ignore rules work for all

---

## Troubleshooting

### Backend Build Fails

**Error:** `COPY backend/bin/cybermesh: no such file`  
**Fix:** Build the Go binary first (see Prerequisites)

### Frontend Build Timeout

**Error:** `npm ci` timeout  
**Fix:** Retry or use `--network=host` for Docker build

### Docker Context Issues

**Error:** Files not found during COPY  
**Fix:** Ensure you're building from project root (`B:\CyberMesh`), not subdirectories

### Permission Denied on Push

**Error:** `denied: Permission denied`  
**Fix:** Run `gcloud auth configure-docker asia-southeast1-docker.pkg.dev`

---

## Maintenance

### Update Image Tags

To use versioned tags instead of `:latest`:

```bash
# Example: v1.0.3
docker build -f docker/backend/Dockerfile -t asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:v1.0.3 .
```

Then update K8s manifests:

```yaml
image: asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:v1.0.3
```

### Clean Old Images

```bash
# List images
gcloud artifacts docker images list asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo

# Delete specific digest
gcloud artifacts docker images delete asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend@sha256:xxxxx
```

---

## CI/CD Integration

For automated builds, use the same commands in your CI pipeline:

```yaml
# Example GitHub Actions
- name: Build Backend
  run: |
    cd backend
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -trimpath -o bin/cybermesh ./cmd/cybermesh/main.go
    cd ..
    docker build -f docker/backend/Dockerfile -t $IMAGE_PATH .
```

---

**Last Updated:** 2025-10-28  
**Version:** 1.0.0
