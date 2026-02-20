# CyberMesh Frontend - Docker Build Guide

## 🎯 Overview

Production-ready Docker setup for CyberMesh frontend (Vite + React + Nginx).

**Features:**
- ✅ Multi-stage build (optimized size ~20MB)
- ✅ Nginx for high-performance serving
- ✅ SPA routing support
- ✅ Security headers configured
- ✅ Health check endpoint
- ✅ Non-root user
- ✅ Gzip compression

---

## 📋 Prerequisites

1. **Docker installed**
   ```bash
   docker --version
   ```

2. **Environment configured**
   - Ensure `cybermesh-frontend/.env` exists with correct Supabase credentials

3. **GCP Project (for pushing to GCR)**
   - Update `Registry` in build scripts with your GCP project ID

---

## 🚀 Quick Start

### Build Image

**Windows (PowerShell):**
```powershell
cd B:\CyberMesh
.\docker\scrits\frontend\build-and-push.ps1 -Tag "v1.0.0"
```

**Linux/Mac (Bash):**
```bash
cd /path/to/CyberMesh
chmod +x docker/scrits/frontend/build-and-push.sh
./docker/scrits/frontend/build-and-push.sh v1.0.0
```

### Test Locally

```bash
docker run -p 8080:8080 gcr.io/YOUR_PROJECT_ID/cybermesh-frontend:v1.0.0

# Open browser
http://localhost:8080
```

### Push to Registry

**PowerShell:**
```powershell
.\docker\scrits\frontend\build-and-push.ps1 -Tag "v1.0.0" -Push
```

**Bash:**
```bash
./docker/scrits/frontend/build-and-push.sh v1.0.0
# Answer 'y' when prompted to push
```

---

## 📦 Files Created

| File | Purpose |
|------|---------|
| `Dockerfile.vite` | Multi-stage build for Vite + Nginx |
| `nginx.conf` | Nginx configuration with SPA routing |
| `.dockerignore` | Excludes unnecessary files from build |
| `../scrits/frontend/build-and-push.ps1` | PowerShell build script |
| `../scrits/frontend/build-and-push.sh` | Bash build script |
| `README.md` | This file |

---

## 🔧 Configuration

### Environment Variables (Build Time)

These are read from `cybermesh-frontend/.env` and embedded at build time:

| Variable | Description | Required |
|----------|-------------|----------|
| `VITE_SUPABASE_PROJECT_ID` | Supabase project ID | ✅ |
| `VITE_SUPABASE_URL` | Supabase project URL | ✅ |
| `VITE_SUPABASE_PUBLISHABLE_KEY` | Supabase anon key | ✅ |
| `VITE_DEMO_MODE` | Demo mode (true/false) | ❌ (default: false) |

**Note:** These values are baked into the static build at build time. To change them, you must rebuild the image.

### Registry Configuration

Update the registry in build scripts:

**PowerShell:**
```powershell
# Line 9 in build-and-push.ps1
[string]$Registry = "gcr.io/YOUR_GCP_PROJECT_ID"
```

**Bash:**
```bash
# Line 12 in build-and-push.sh
REGISTRY="gcr.io/YOUR_GCP_PROJECT_ID"
```

---

## 🐳 Manual Docker Commands

### Build
```bash
docker build \
  -f docker/frontend/Dockerfile.vite \
  -t cybermesh-frontend:latest \
  --build-arg VITE_SUPABASE_PROJECT_ID=wcgddjipyslnjstabqaq \
  --build-arg VITE_SUPABASE_URL=https://wcgddjipyslnjstabqaq.supabase.co \
  --build-arg VITE_SUPABASE_PUBLISHABLE_KEY=your-key-here \
  --build-arg VITE_DEMO_MODE=false \
  .
```

### Run
```bash
docker run -d \
  -p 8080:8080 \
  --name cybermesh-frontend \
  cybermesh-frontend:latest
```

### Health Check
```bash
curl http://localhost:8080/health
# Expected: "healthy"
```

### View Logs
```bash
docker logs -f cybermesh-frontend
```

### Stop & Remove
```bash
docker stop cybermesh-frontend
docker rm cybermesh-frontend
```

---

## 🎭 Nginx Configuration Highlights

### SPA Routing
All routes fall back to `index.html` for client-side routing:
```nginx
location / {
    try_files $uri $uri/ /index.html;
}
```

### Caching Strategy
- **Static assets** (JS, CSS, images): 1 year cache
- **index.html**: No cache (always fresh)

### Security Headers
- X-Frame-Options: SAMEORIGIN
- X-Content-Type-Options: nosniff
- X-XSS-Protection: enabled
- Referrer-Policy: strict-origin-when-cross-origin

### Compression
Gzip enabled for text/JSON/JavaScript files (6x compression)

---

## 🚀 Deployment to GKE

### 1. Build and Push
```powershell
.\docker\scrits\frontend\build-and-push.ps1 -Tag "v1.0.0" -Push
```

### 2. Update K8s Deployment
```yaml
# k8s_gke/frontend-deployment.yaml
spec:
  containers:
  - name: frontend
    image: gcr.io/YOUR_PROJECT_ID/cybermesh-frontend:v1.0.0
    ports:
    - containerPort: 8080
```

### 3. Deploy
```bash
kubectl apply -f k8s_gke/frontend-deployment.yaml
kubectl apply -f k8s_gke/frontend-service.yaml
```

---

## 🔍 Troubleshooting

### Build Fails - "index.html not found"
**Cause:** Vite build failed  
**Fix:** Check if `npm run build` works locally in `cybermesh-frontend/`

### Image Too Large
**Current:** ~20MB  
**If larger:** Check `.dockerignore` is excluding node_modules and dist

### 404 on Routes
**Cause:** Nginx not configured for SPA routing  
**Fix:** Ensure `nginx.conf` has `try_files $uri $uri/ /index.html;`

### Health Check Failing
**Cause:** Nginx not starting or port mismatch  
**Fix:** Check logs: `docker logs <container-id>`

### CORS Errors
**Cause:** Supabase edge functions rejecting requests  
**Fix:** Verify CORS headers in `nginx.conf` and Supabase CORS config

---

## 📊 Image Details

| Metric | Value |
|--------|-------|
| Base Image | nginx:1.25-alpine |
| Final Size | ~20MB |
| Port | 8080 |
| Health Check | `/health` endpoint |
| User | nginx (non-root, UID 101) |

---

## 🎉 Next Steps

1. ✅ Build image locally
2. ✅ Test image locally (`docker run`)
3. ✅ Update registry in build scripts
4. ✅ Push to GCR/Docker Hub
5. ✅ Deploy to GKE
6. ✅ Verify frontend works in production

**Questions?** Check the main project README or deployment guides in `k8s_gke/`.
