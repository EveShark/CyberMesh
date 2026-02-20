# CyberMesh Frontend - GKE Deployment Guide

## 🎯 Overview

Complete guide for building and deploying the CyberMesh frontend to Google Kubernetes Engine (GKE) using Artifact Registry.

**Image Registry:**
```
us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend
```

**Deployment Details:**
- Port: `3000` (matches GKE deployment)
- User: `UID 1001` (non-root, matches K8s securityContext)
- Health endpoints: `/health` and `/healthz`
- Base image: `nginx:1.25-alpine`
- Final size: ~20MB

---

## 📋 Prerequisites

1. **Docker installed and running**
   ```powershell
   docker --version
   ```

2. **Google Cloud SDK authenticated**
   ```powershell
   gcloud auth login
   gcloud auth configure-docker us-central1-docker.pkg.dev
   ```

3. **kubectl configured for GKE cluster**
   ```powershell
   gcloud container clusters get-credentials YOUR_CLUSTER --region us-central1
   ```

4. **Environment file configured**
   - `cybermesh-frontend/.env` must exist with:
     - `VITE_SUPABASE_PROJECT_ID`
     - `VITE_SUPABASE_URL`
     - `VITE_SUPABASE_PUBLISHABLE_KEY`
     - `VITE_DEMO_MODE`

---

## 🚀 Quick Deploy (3 Commands)

### 1. Build and Push Image
```powershell
cd B:\CyberMesh
.\docker\frontend\build-and-push.ps1 -Tag "v1.0.0" -Push
```

### 2. Update K8s Deployment
```powershell
kubectl set image deployment/frontend frontend=us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend:v1.0.0 -n cybermesh
```

### 3. Verify Deployment
```powershell
kubectl rollout status deployment/frontend -n cybermesh
kubectl get pods -n cybermesh -l app=frontend
```

---

## 📦 Step-by-Step Build Process

### Step 1: Verify Environment

```powershell
# Check .env file exists
Test-Path cybermesh-frontend\.env

# View Supabase config
Get-Content cybermesh-frontend\.env | Select-String "VITE_SUPABASE"
```

**Expected output:**
```
VITE_SUPABASE_PROJECT_ID=wcgddjipyslnjstabqaq
VITE_SUPABASE_URL=https://wcgddjipyslnjstabqaq.supabase.co
VITE_SUPABASE_PUBLISHABLE_KEY=eyJh...
```

### Step 2: Build Docker Image

```powershell
cd B:\CyberMesh

# Build with default tag (latest)
.\docker\frontend\build-and-push.ps1

# OR build with specific version tag
.\docker\frontend\build-and-push.ps1 -Tag "v1.0.0"
```

**What happens:**
1. Reads environment variables from `.env`
2. Builds Vite app with embedded env vars
3. Creates nginx-based production image
4. Tags image with GKE Artifact Registry path

**Build time:** ~2-5 minutes

### Step 3: Test Locally (Optional)

```powershell
# Run container
docker run -d -p 3000:3000 --name test-frontend `
  us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend:v1.0.0

# Test health endpoint
curl http://localhost:3000/health
# Expected: "healthy"

# Open in browser
Start-Process http://localhost:3000

# View logs
docker logs -f test-frontend

# Cleanup
docker stop test-frontend
docker rm test-frontend
```

### Step 4: Push to Artifact Registry

```powershell
# Push with -Push flag
.\docker\frontend\build-and-push.ps1 -Tag "v1.0.0" -Push

# OR push manually
docker push us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend:v1.0.0
```

**Push time:** ~1-3 minutes

---

## 🎛️ Deployment to GKE

### Method 1: Using kubectl (Recommended)

```powershell
# Update deployment with new image
kubectl set image deployment/frontend `
  frontend=us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend:v1.0.0 `
  -n cybermesh

# Watch rollout
kubectl rollout status deployment/frontend -n cybermesh

# Check pods
kubectl get pods -n cybermesh -l app=frontend -w
```

### Method 2: Using deployment YAML

```powershell
# Edit frontend-deployment.yaml
# Change line 52:
# image: us-central1-docker.pkg.dev/.../cybermesh-frontend:v1.0.0

kubectl apply -f k8s_gke/frontend-deployment.yaml
```

### Method 3: Using prepare_for_gke.ps1 Script

```powershell
cd k8s_gke

# Update all manifests with new image tag
.\prepare_for_gke.ps1 -ImageTag "v1.0.0" -Region "us-central1"

# Apply updated manifests
kubectl apply -f frontend-deployment.yaml
```

---

## 🔍 Verification & Testing

### Check Deployment Status

```powershell
# Pod status
kubectl get pods -n cybermesh -l app=frontend

# Deployment details
kubectl describe deployment frontend -n cybermesh

# View logs
kubectl logs -f deployment/frontend -n cybermesh

# Check events
kubectl get events -n cybermesh --sort-by='.lastTimestamp' | Select-Object -Last 10
```

### Test Health Endpoints

```powershell
# Get pod name
$POD = kubectl get pods -n cybermesh -l app=frontend -o jsonpath='{.items[0].metadata.name}'

# Test health endpoint
kubectl exec -n cybermesh $POD -- curl -s http://localhost:3000/health

# Test healthz endpoint
kubectl exec -n cybermesh $POD -- curl -s http://localhost:3000/healthz
```

### Access Frontend

```powershell
# Port-forward to local
kubectl port-forward -n cybermesh deployment/frontend 3000:3000

# Open browser
Start-Process http://localhost:3000
```

**OR via LoadBalancer:**
```powershell
# Get external IP
kubectl get svc frontend -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# Open in browser
Start-Process http://<EXTERNAL_IP>
```

---

## 🐛 Troubleshooting

### Issue: Build Fails - "index.html not found"

**Cause:** Vite build failed

**Solution:**
```powershell
cd cybermesh-frontend
npm install
npm run build
# Check if dist/index.html exists
```

### Issue: ImagePullBackOff

**Cause:** GKE can't pull image from Artifact Registry

**Solution:**
```powershell
# Verify image exists
gcloud artifacts docker images list us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo

# Check permissions
gcloud artifacts repositories describe cybermesh-repo --location=us-central1

# Verify Workload Identity is configured
kubectl describe serviceaccount default -n cybermesh
```

### Issue: Pod CrashLoopBackOff

**Cause:** Nginx failing to start

**Solution:**
```powershell
# Check logs
kubectl logs -n cybermesh -l app=frontend --tail=50

# Common causes:
# 1. Port conflict (should be 3000)
# 2. Permission issues (should run as UID 1001)
# 3. Missing files in /usr/share/nginx/html
```

### Issue: Health Check Failing

**Cause:** K8s probes can't reach endpoints

**Solution:**
```powershell
# Check probe configuration in deployment
kubectl get deployment frontend -n cybermesh -o yaml | Select-String -Pattern "probe" -Context 3

# Should be:
# livenessProbe:
#   httpGet:
#     path: /
#     port: 3000

# Test manually
kubectl exec -n cybermesh <pod-name> -- curl -v http://localhost:3000/health
```

### Issue: 404 on Routes

**Cause:** Nginx not handling SPA routing

**Fix:** Verify `nginx.conf` has:
```nginx
location / {
    try_files $uri $uri/ /index.html;
}
```

Rebuild if needed.

---

## 📊 Build Parameters

### Default Values

| Parameter | Default Value | Description |
|-----------|---------------|-------------|
| `Tag` | `latest` | Image tag |
| `Region` | `us-central1` | GCP region |
| `ProjectId` | `sunny-vehicle-482107-p5` | GCP project |
| `Repository` | `cybermesh-repo` | Artifact Registry repo |

### Custom Build Examples

```powershell
# Development build
.\build-and-push.ps1 -Tag "dev-$(Get-Date -Format 'yyyyMMdd-HHmm')"

# Production build with different region
.\build-and-push.ps1 -Tag "prod-v1.0.0" -Region "asia-southeast1" -Push

# Build without pushing
.\build-and-push.ps1 -Tag "test-local"
```

---

## 🔄 Rollback Procedure

### Rollback to Previous Version

```powershell
# View rollout history
kubectl rollout history deployment/frontend -n cybermesh

# Rollback to previous version
kubectl rollout undo deployment/frontend -n cybermesh

# Rollback to specific revision
kubectl rollout undo deployment/frontend -n cybermesh --to-revision=2

# Verify rollback
kubectl rollout status deployment/frontend -n cybermesh
```

---

## 📝 Environment Variables

### Build-Time Variables (Embedded in Image)

These are read from `.env` and baked into the static build:

| Variable | Required | Description |
|----------|----------|-------------|
| `VITE_SUPABASE_PROJECT_ID` | ✅ | Supabase project identifier |
| `VITE_SUPABASE_URL` | ✅ | Supabase API endpoint |
| `VITE_SUPABASE_PUBLISHABLE_KEY` | ✅ | Supabase anon/public key |
| `VITE_DEMO_MODE` | ❌ | Demo mode flag (default: false) |

**Important:** To change these values, you must **rebuild the image**. They cannot be changed at runtime.

---

## 🎉 Success Checklist

- [ ] `.env` file configured with correct Supabase credentials
- [ ] Docker authenticated with Artifact Registry
- [ ] Image built successfully
- [ ] Image pushed to Artifact Registry
- [ ] Deployment updated with new image tag
- [ ] Pods running and healthy
- [ ] Health endpoints responding
- [ ] Frontend accessible via LoadBalancer
- [ ] All routes working (SPA routing)
- [ ] No console errors

---

## 🔗 Related Documentation

- [Main Docker README](./README.md)
- [GKE Setup Guide](../../k8s_gke/GKE_SETUP_GUIDE.md)
- [Frontend Architecture](../../cybermesh-frontend/docs/ARCHITECTURE.md)
- [Deployment Guide](../../k8s_gke/deploy_to_gke.sh)

---

## 📞 Support

**Common Commands Reference:**

```powershell
# Build
.\docker\frontend\build-and-push.ps1 -Tag "v1.0.0"

# Push
.\docker\frontend\build-and-push.ps1 -Tag "v1.0.0" -Push

# Deploy
kubectl set image deployment/frontend frontend=us-central1-docker.pkg.dev/sunny-vehicle-482107-p5/cybermesh-repo/cybermesh-frontend:v1.0.0 -n cybermesh

# Verify
kubectl get pods -n cybermesh -l app=frontend
kubectl logs -f deployment/frontend -n cybermesh

# Rollback
kubectl rollout undo deployment/frontend -n cybermesh
```

**Need help?** Check the troubleshooting section or K8s logs.
