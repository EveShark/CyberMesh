# CyberMesh GKE Deployment Guide

Quick reference for deploying the CyberMesh consensus backend to Google Kubernetes Engine (GKE) Autopilot.

---

## Overview

**What we deployed:**
- 5-node PBFT consensus cluster
- Multi-zone HA in `asia-southeast1`
- Artifact Registry for container images
- GKE Autopilot (fully managed)

**Resources per pod:**
- Memory: 1GB request, 2GB limit
- CPU: 0.5 cores request, 2 cores limit
- Storage: 10GB SSD per pod (50GB total)

**Total cluster capacity:**
- Memory: 5GB guaranteed, 10GB max
- CPU: 2.5 cores guaranteed, 10 cores max

---

## Prerequisites

1. **GCP Project**: `cybermesh-476310`
2. **gcloud CLI** authenticated
3. **Docker Desktop** (for building images)
4. **kubectl** installed

---

## Step 1: Build & Push Image

### Build in Docker Desktop (not Rancher)

```bash
# Build image (182MB)
cd /path/to/CyberMesh
docker build -t cybermesh/cybermesh-backend:latest .

# Verify build
docker images | grep cybermesh-backend
```

### Tag & Push to Artifact Registry

```bash
# Set project
gcloud config set project cybermesh-476310

# Enable APIs
gcloud services enable artifactregistry.googleapis.com container.googleapis.com

# Create repository (one-time)
gcloud artifacts repositories create cybermesh-repo \
  --repository-format=docker \
  --location=us-central1

# Configure Docker auth
gcloud auth configure-docker asia-southeast1-docker.pkg.dev

# Tag for Artifact Registry
docker tag cybermesh/cybermesh-backend:latest \
  asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest

# Push (using Docker Desktop context)
export DOCKER_HOST=''
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest
```

**Why Docker Desktop?** Rancher Desktop has network routing issues with GCR/Artifact Registry.

---

## Step 2: Create GKE Autopilot Cluster

```bash
# Create regional cluster (3 zones)
gcloud container clusters create-auto cybermesh-cluster \
  --region=asia-southeast1 \
  --project=cybermesh-476310

# Verify kubectl context
kubectl config current-context
# Should show: gke_cybermesh-476310_asia-southeast1_cybermesh-cluster
```

**Cluster details:**
- **Type**: Autopilot (Google manages nodes)
- **Zones**: asia-southeast1-a, b, c
- **Auto-scaling**: Nodes provision on-demand
- **Cost**: ~$72/month base + pod resources

---

## Step 3: Deploy Manifests

```bash
cd k8s

# Deploy in order
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f configmap-db-cert.yaml
kubectl apply -f secret.yaml
kubectl apply -f service-headless.yaml
kubectl apply -f service.yaml
kubectl apply -f pdb.yaml
kubectl apply -f statefulset.yaml
```

**Or deploy all at once:**
```bash
kubectl apply -f .
```

---

## Step 4: Verify Deployment

### Check pod status
```bash
kubectl get pods -n cybermesh

# Expected output:
# NAME          READY   STATUS    RESTARTS   AGE
# validator-0   1/1     Running   0          5m
# validator-1   1/1     Running   0          5m
# validator-2   1/1     Running   0          5m
# validator-3   1/1     Running   0          5m
# validator-4   1/1     Running   0          5m
```

### Check services
```bash
kubectl get svc -n cybermesh

# Note the EXTERNAL-IP for validator-api
```

### Check persistent volumes
```bash
kubectl get pvc -n cybermesh

# Should show 5 PVCs (10Gi each, premium-rwo)
```

---

## Accessing the API

### Get LoadBalancer IP
```bash
export LB_IP=$(kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo $LB_IP
```

### Test endpoints
```bash
# Health check
curl http://$LB_IP:9441/api/v1/health

# Metrics
curl http://$LB_IP:9100/metrics
```

---

## Debugging & Logs

### View logs
```bash
# Single pod
kubectl logs -n cybermesh validator-0 -f

# All validators
kubectl logs -n cybermesh -l component=validator --tail=50

# Specific pod with timestamps
kubectl logs -n cybermesh validator-0 --timestamps --tail=100
```

### Check pod details
```bash
# Describe pod (events, status, resources)
kubectl describe pod validator-0 -n cybermesh

# Check init container logs
kubectl logs -n cybermesh validator-0 -c wait-for-db
```

### Exec into pod
```bash
# Interactive shell
kubectl exec -n cybermesh validator-0 -it -- /bin/sh

# Inside pod:
# - View logs: tail -f /app/logs/application.log
# - Check certs: ls -la /app/certs/
# - Check keys: ls -la /app/keys/
# - Test DB: nc -zv cybermesh-threats-8958.jxf.gcp-asia-southeast1.cockroachlabs.cloud 26257
```

### Check resource usage
```bash
kubectl top pods -n cybermesh
kubectl top nodes
```

### Check events
```bash
# Namespace events
kubectl get events -n cybermesh --sort-by='.lastTimestamp'

# Specific pod events
kubectl get events -n cybermesh --field-selector involvedObject.name=validator-0
```

---

## Common Issues

### Pods stuck in Pending
**Symptom:** Pods show `Pending` status for >5 minutes

**Check:**
```bash
kubectl describe pod validator-0 -n cybermesh | grep -A 10 Events
```

**Common causes:**
- Insufficient cluster resources (Autopilot auto-scales, wait 2-3 min)
- PVC provisioning delay (check `kubectl get pvc -n cybermesh`)
- Image pull errors (verify image exists in Artifact Registry)

### Image pull errors
**Symptom:** `ImagePullBackOff` or `ErrImagePull`

**Fix:**
```bash
# Verify image exists
gcloud artifacts docker images list \
  asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo

# GKE should auto-configure Artifact Registry access, but if needed:
kubectl create secret docker-registry gcr-secret \
  --docker-server=asia-southeast1-docker.pkg.dev \
  --docker-username=_json_key \
  --docker-password="$(gcloud auth print-access-token)" \
  -n cybermesh
```

### Pods crash looping
**Symptom:** `CrashLoopBackOff` status

**Check logs:**
```bash
kubectl logs -n cybermesh validator-0 --previous
```

**Common causes:**
- Database connection failure (check CockroachDB connectivity)
- Missing secrets/config (verify `kubectl get secrets -n cybermesh`)
- Invalid signing keys

### LoadBalancer stuck at `<pending>`
**Symptom:** External IP shows `<pending>`

**Check:**
```bash
kubectl describe svc validator-api -n cybermesh
```

GKE typically provisions LoadBalancers in 2-3 minutes. If stuck >5 min, check GCP quotas.

---

## Scaling

### Scale replicas
```bash
# Scale to 7 nodes (f=2 Byzantine fault tolerance)
kubectl scale statefulset validator -n cybermesh --replicas=7

# Watch rollout
kubectl rollout status statefulset/validator -n cybermesh
```

**Note:** Update `CONSENSUS_NODES` in ConfigMap when scaling.

---

## Updates & Rollouts

### Update image
```bash
# Push new image with tag
docker tag cybermesh/cybermesh-backend:latest \
  asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:v1.1.0
docker push asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:v1.1.0

# Update StatefulSet
kubectl set image statefulset/validator \
  validator=asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:v1.1.0 \
  -n cybermesh

# Watch rolling update
kubectl rollout status statefulset/validator -n cybermesh
```

### Rollback
```bash
kubectl rollout undo statefulset/validator -n cybermesh
```

---

## Cleanup

### Delete namespace (removes everything)
```bash
kubectl delete namespace cybermesh
```

### Delete cluster
```bash
gcloud container clusters delete cybermesh-cluster \
  --region=asia-southeast1 \
  --project=cybermesh-476310 \
  --quiet
```

### Delete Artifact Registry repository
```bash
gcloud artifacts repositories delete cybermesh-repo \
  --location=us-central1 \
  --project=cybermesh-476310 \
  --quiet
```

---

## Current Deployment Status

**Cluster:**
- Name: `cybermesh-cluster`
- Region: `asia-southeast1`
- Status: ✅ Running
- Master IP: `34.158.43.136`

**Pods:**
- validator-0 to validator-4: ✅ Running (1/1)
- Image: `asia-southeast1-docker.pkg.dev/cybermesh-476310/cybermesh-repo/cybermesh-backend:latest`
- Size: 182MB

**Services:**
- LoadBalancer IP: `34.143.137.254`
- API Port: `9441`
- Metrics Port: `9100`

**Storage:**
- 5x 10Gi SSD volumes (`premium-rwo`)
- Total: 50GB allocated

---

## Key Configuration Files

| File | Purpose |
|------|---------|
| `namespace.yaml` | Creates `cybermesh` namespace |
| `rbac.yaml` | ServiceAccount + Role for K8s API access |
| `configmap.yaml` | Environment variables (362 lines) |
| `configmap-db-cert.yaml` | CockroachDB root certificate |
| `secret.yaml` | Sensitive data (DB, Kafka, Redis, keys) |
| `service.yaml` | LoadBalancer for external access |
| `service-headless.yaml` | P2P service discovery |
| `pdb.yaml` | PodDisruptionBudget (min 3 available) |
| `statefulset.yaml` | Main deployment (5 validator pods) |

---

## Architecture Highlights

**Multi-zone HA:**
- Pods spread across 3 zones via `topologyKey: topology.kubernetes.io/zone`
- Survives single zone failure

**Security:**
- Non-root containers (`runAsUser: 1000`)
- Read-only root filesystem (where possible)
- Dropped all capabilities
- Secrets mounted as volumes

**Health checks:**
- Startup probe: 12 attempts × 5s (1 min grace period)
- Liveness probe: checks `/api/v1/health` every 10s
- Readiness probe: checks every 5s

**Storage:**
- Premium SSD (`premium-rwo`) for log persistence
- 10GB per pod with log rotation (max 100MB/file, 10 backups, 30 days)

---

## Support

**GCP Console:**
https://console.cloud.google.com/kubernetes/workload_/gcloud/asia-southeast1/cybermesh-cluster?project=cybermesh-476310

**Logs:**
```bash
kubectl logs -n cybermesh -l app=cybermesh-backend --tail=100 -f
```

**Monitoring:**
- Metrics endpoint: `http://<LB_IP>:9100/metrics`
- GCP Monitoring: Auto-enabled for Autopilot

---

**Last updated:** 2025-10-07
**Deployed by:** Claude Code
**Cluster version:** 1.33.4-gke.1245000
