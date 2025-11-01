# CyberMesh Docker Images

Docker build configurations for CyberMesh services optimized for OCI Container Registry.

## Structure

```
docker/
├── backend/
│   ├── Dockerfile           # Backend consensus service
│   ├── .dockerignore
│   └── bin/
│       └── cybermesh       # Pre-built Go binary
│
├── ai-service/
│   ├── Dockerfile           # AI detection service
│   └── .dockerignore
│
└── README.md                # This file
```

## Image Strategy

- **Tag:** Always use `:latest` (no version tags)
- **Registry:** OCI Container Registry
- **Pull Policy:** `imagePullPolicy: Always` in K8s
- **Base Images:** Debian 12 (bookworm) for consistency

## Build Commands

### Backend (Consensus Service)

```bash
# Build (from project root)
docker build -t cybermesh-backend:latest -f docker/backend/Dockerfile docker/backend/

# Tag for OCI Registry
docker tag cybermesh-backend:latest \
  <region>.ocir.io/<tenancy-namespace>/cybermesh/backend:latest

# Push to OCI
docker push <region>.ocir.io/<tenancy-namespace>/cybermesh/backend:latest
```

**Build Context:** `docker/backend/` (contains pre-built binary; TLS certs mounted at runtime)

### AI Service (Detection & ML Pipeline)

```bash
# Build (from project root - needs ai-service/ directory)
docker build -t cybermesh-ai-service:latest -f docker/ai-service/Dockerfile .

# Tag for OCI Registry
docker tag cybermesh-ai-service:latest \
  <region>.ocir.io/<tenancy-namespace>/cybermesh/ai-service:latest

# Push to OCI
docker push <region>.ocir.io/<tenancy-namespace>/cybermesh/ai-service:latest
```

**Build Context:** Project root `.` (needs `ai-service/` directory)

## Image Sizes

| Service | Base Image | Dependencies | Final Size |
|---------|-----------|--------------|------------|
| Backend | debian:12-slim | None | ~80 MB |
| AI Service | python:3.11-slim | ML libs (300MB) | ~480 MB |

**Note:** AI service does NOT include models (38MB+ files) - they're mounted from OCI Object Storage at runtime.

## Runtime Requirements

### Backend
- **Ports:** 9441 (API), 9100 (metrics)
- **User:** cybermesh:1000
- **Required at Runtime:**
  - ConfigMap → Environment variables (all config)
  - Secret → Signing keys at `/app/keys/` or via env var
  - Secret → CockroachDB TLS cert at `/app/certs/root.crt` (MUST be mounted, not baked in image)
- **Volumes:**
  - ConfigMap → Environment variables
  - Secret (DB cert) → `/app/certs/root.crt` (subPath mount)
  - Secret (signing keys) → `/tmp/keys/` (created from env var in K8s startup script)
  - EmptyDir → `/app/logs/`

### AI Service
- **Ports:** 8000 (API), 10000 (metrics)
- **User:** aiservice:1001
- **Entrypoint:** `/app/entrypoint.sh` (pre-flight validation)
- **Required Environment Variables:**
  - `NODE_ID`
  - `ENVIRONMENT`
  - `KAFKA_BOOTSTRAP_SERVERS`
  - `REDIS_HOST`, `REDIS_PORT`
  - `ED25519_SIGNING_KEY_ID`
  - `ED25519_DOMAIN_SEPARATION`
  - `AI_SIGNING_KEY_CONTENT` (K8s Secret - signing key PEM content)
- **Volumes:**
  - ConfigMap → Environment variables (not file-based)
  - Secret → `AI_SIGNING_KEY_CONTENT` env var (converted to file at `/tmp/keys/signing_key.pem`)
  - OCI Object Storage → `/app/data/models/` (ReadOnly)
  - OCI Object Storage → `/app/data/datasets/` (ReadOnly)
  - EmptyDir → `/app/data/` (nonce_state.json)
  - EmptyDir → `/app/logs/`

## Health Checks

### Backend
```bash
curl -f http://localhost:9441/api/v1/health
```

### AI Service
```bash
curl -f http://localhost:8000/health
```

## Quick Build & Push (Both Services)

```bash
# Backend
cd docker/backend && \
docker build -t <region>.ocir.io/<namespace>/cybermesh/backend:latest . && \
docker push <region>.ocir.io/<namespace>/cybermesh/backend:latest

# AI Service (from project root)
docker build -t <region>.ocir.io/<namespace>/cybermesh/ai-service:latest \
  -f docker/ai-service/Dockerfile . && \
docker push <region>.ocir.io/<namespace>/cybermesh/ai-service:latest
```

## CI/CD Integration

Since we use `:latest` tag, K8s deployments need:

```yaml
spec:
  containers:
  - name: backend
    image: <region>.ocir.io/<namespace>/cybermesh/backend:latest
    imagePullPolicy: Always  # Force pull latest on pod restart
```

To update:
```bash
# Push new image
docker push <region>.ocir.io/<namespace>/cybermesh/backend:latest

# Restart deployment (pulls latest)
kubectl rollout restart deployment/backend -n cybermesh
```

## Troubleshooting

**Build Context Errors:**
- Backend: Must run from `docker/backend/` or specify context
- AI Service: Must run from project root (needs `ai-service/` directory)

**Image Size Too Large:**
- Check `.dockerignore` files
- AI Service: Verify models are excluded (should NOT be in image)

**OCI Push Fails:**
- Ensure `docker login` to OCI registry
- Check namespace and region are correct
- Verify IAM policies allow pushing

**AI Service Startup Failures:**

*"Required environment variable X is not set"*
- Check your K8s ConfigMap has all required variables
- Verify ConfigMap is referenced in deployment spec

*"No signing key available"*
- Ensure K8s Secret contains `AI_SIGNING_KEY_CONTENT`
- Check secret is mounted as environment variable (not file)
- Verify secret is in correct namespace

*"Model directory not found"*
- Verify OCI Object Storage bucket is mounted at `/app/data/models/`
- Check PVC/volume claim is bound
- Ensure init container downloaded models successfully

*"Cannot connect to Redis"*
- Check Redis service is running and accessible
- Verify REDIS_HOST and REDIS_PORT are correct
- Check network policies allow pod → Redis traffic

*"Cannot reach Kafka broker"*
- Entrypoint now performs a TCP check; verify bootstrap host/port and network rules

**Health Check Fails:**
- Backend: Ensure port 9441 is exposed
- AI Service: Check models are mounted (won't start without them)
- AI Service: Entrypoint validates connectivity before starting Python

## Development vs Production

**Development:**
```bash
# Build locally, don't push
docker build -t backend:latest -f docker/backend/Dockerfile docker/backend/
docker run -p 9441:9441 backend:latest
```

**Production:**
```bash
# Build, tag, and push to OCI
docker build -t backend:latest -f docker/backend/Dockerfile docker/backend/
docker tag backend:latest <oci-registry>/cybermesh/backend:latest
docker push <oci-registry>/cybermesh/backend:latest
```

## Docker Configuration Summary

### ✅ Validated Changes

**Backend:**
- ✅ TLS certificates NOT baked into image (removed `COPY certs/` line)
- ✅ Certificates MUST be mounted at runtime via K8s Secret at `/app/certs/root.crt`
- ✅ Runtime directories created with correct ownership: `/app/keys`, `/app/logs`
- ✅ Image size: ~80 MB (minimal, no certs/keys)

**AI Service:**
- ✅ Entrypoint script properly configured (`ENTRYPOINT ["/app/entrypoint.sh"]`)
- ✅ Pre-flight validation: Environment variables, signing keys, Redis, Kafka
- ✅ Kafka multi-broker parsing fixed (handles comma-separated bootstrap servers)
- ✅ NO CockroachDB connection (no cert mount needed)
- ✅ Image size: ~480 MB (Python + ML libs, NO models)

**Entrypoint Script (`ai-service/entrypoint.sh`):**
- ✅ Full environment variable validation (7 required vars)
- ✅ Creates signing key from `AI_SIGNING_KEY_CONTENT` env var → `/tmp/keys/signing_key.pem`
- ✅ Validates model directory exists (OCI Object Storage mount)
- ✅ Redis connectivity check (TCP, 5s timeout)
- ✅ Kafka connectivity check (TCP, first broker from comma-separated list, 5s timeout)
- ✅ Fails fast with clear error messages

### ⚠️ Critical K8s Requirements

**Backend StatefulSet MUST provide:**
```yaml
volumeMounts:
- name: db-certs
  mountPath: /app/certs/root.crt
  subPath: root.crt
  readOnly: true
volumes:
- name: db-certs
  secret:
    secretName: cockroachdb-root-cert
    items:
    - key: root.crt
      path: root.crt
```

**AI Service Deployment MUST provide:**
```yaml
env:
- name: AI_SIGNING_KEY_CONTENT
  valueFrom:
    secretKeyRef:
      name: ai-service-secret
      key: signing_key_pem
# ... plus all required env vars from ConfigMap

volumeMounts:
- name: models
  mountPath: /app/data/models
  readOnly: true
volumes:
- name: models
  # OCI Object Storage bucket mount
```

### Design Decisions

1. **No baked secrets:** All certificates and keys provided at runtime
2. **Environment-based config:** No `.env` files in images, everything via K8s ConfigMap
3. **Fail-fast validation:** Entrypoint catches misconfigurations before application starts
4. **Consistent patterns:** Both services use env var → file conversion for keys
5. **OCI Object Storage:** Models/datasets NOT in image, mounted at runtime
6. **Latest tag only:** Simplified CI/CD with `imagePullPolicy: Always`
