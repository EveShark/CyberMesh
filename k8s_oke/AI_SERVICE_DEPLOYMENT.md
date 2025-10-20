# CyberMesh AI Service - Kubernetes Deployment Guide

## Overview

Deploys AI ML detection service (1 pod initially) to existing GKE/OKE cluster alongside 5 backend validators.

**Architecture:**
- Deployment (stateless, horizontally scalable)
- ClusterIP service (internal only)
- Dedicated RBAC (Kafka/Redis access only, NO database)
- Volume-mounted signing key (Ed25519)
- OCI Object Storage for models (placeholder ready)

---

## Port Assignments (NO CONFLICTS)

| Service | API Port | P2P Port | Metrics Port |
|---------|----------|----------|--------------|
| Backend Validators | 9441 | 8001 | 9100 |
| AI Service | 8080 | N/A | 10000 |

---

## Prerequisites

### 1. Cluster Must Have:
- ✅ Namespace: `cybermesh` (already exists)
- ✅ Secret: `cybermesh-secrets` (contains Kafka/Redis creds)
- ✅ 5 backend validators running
- ⚠️ OCI CSI driver (optional, for Object Storage mounts)

### 2. Docker Image:
```bash
# Build AI service image (if not already built)
cd B:\CyberMesh
docker build -t ai-service:latest -f docker/ai-service/Dockerfile .

# Tag for GCP Artifact Registry (or OCI Registry)
docker tag ai-service:latest \
  us-central1-docker.pkg.dev/cybermesh-474414/cybermesh-repo/ai-service:latest

# Push
docker push us-central1-docker.pkg.dev/cybermesh-474414/cybermesh-repo/ai-service:latest
```

### 3. Verify Existing Resources:
```bash
# Check namespace
kubectl get namespace cybermesh

# Check backend secrets (AI reuses Kafka/Redis creds)
kubectl get secret cybermesh-secrets -n cybermesh

# Check backend pods
kubectl get pods -n cybermesh -l app=consensus-backend
```

---

## Deployment Steps

### Step 1: Create AI RBAC (ServiceAccount + Role + RoleBinding)
```bash
kubectl apply -f k8s/ai-service-rbac.yaml

# Verify
kubectl get serviceaccount ai-service-sa -n cybermesh
kubectl get role ai-service-role -n cybermesh
kubectl get rolebinding ai-service-rolebinding -n cybermesh
```

**What this does:**
- Creates dedicated ServiceAccount for AI service
- Grants minimal permissions: read pods/services/endpoints
- NO access to database secrets or validator keys

---

### Step 2: Create AI ConfigMap
```bash
kubectl apply -f k8s/ai-service-configmap.yaml

# Verify
kubectl get configmap ai-service-config -n cybermesh
kubectl describe configmap ai-service-config -n cybermesh | head -20
```

**What this contains:**
- All non-secret AI configuration from `ai-service/.env`
- Kafka brokers, Redis endpoints, detection settings
- Model paths, API settings, logging config
- Separate from backend ConfigMap for clean boundaries

---

### Step 3: Create AI Secret (Signing Key)
```bash
kubectl apply -f k8s/ai-service-secret.yaml

# Verify
kubectl get secret ai-service-secret -n cybermesh
kubectl describe secret ai-service-secret -n cybermesh
```

**What this contains:**
- AI Ed25519 signing key (from `keys/signing_key.pem`)
- Base64 encoded, mounted as volume (NOT env var)
- Kafka/Redis passwords referenced from existing `cybermesh-secrets`

---

### Step 4: Deploy AI Service (1 Replica)
```bash
kubectl apply -f k8s/ai-service-deployment.yaml

# Watch deployment rollout
kubectl rollout status deployment/ai-service -n cybermesh

# Check pod status
kubectl get pods -n cybermesh -l app=ai-service
```

**Expected output:**
```
NAME                          READY   STATUS    RESTARTS   AGE
ai-service-xxxxxxxxxx-xxxxx   1/1     Running   0          30s
```

**What this creates:**
- 1 AI service pod (resource: 2-4GB RAM, 500m-2000m CPU)
- Mounts signing key from Secret at `/app/keys/signing_key.pem`
- Liveness/readiness probes on port 8080
- Models included in Docker image (for now)

---

### Step 5: Create AI Service (ClusterIP)
```bash
kubectl apply -f k8s/ai-service-service.yaml

# Verify
kubectl get service ai-service -n cybermesh
```

**Expected output:**
```
NAME         TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)            AGE
ai-service   ClusterIP   10.xxx.xxx.xxx   <none>        8080/TCP,10000/TCP 10s
```

**What this does:**
- Internal ClusterIP service (no external access)
- DNS: `ai-service.cybermesh.svc.cluster.local:8080`
- Backend can reference via service name

---

## Verification & Testing

### 1. Check Pod Logs
```bash
# Follow AI service logs
kubectl logs -f -n cybermesh -l app=ai-service

# Look for:
# ✓ "Configuration loaded successfully"
# ✓ "Ed25519 signing key loaded"
# ✓ "Connected to Kafka: pkc-ldvr1..."
# ✓ "Connected to Redis: merry-satyr..."
# ✓ "Models loaded: ddos, malware, anomaly"
# ✓ "DetectionLoop started (interval: 5s)"
```

### 2. Check Health Endpoints
```bash
# Port-forward to AI service
kubectl port-forward -n cybermesh svc/ai-service 8080:8080 &

# Test health endpoint
curl http://localhost:8080/health
# Expected: {"status":"ok","service":"ai-service"}

# Test ready endpoint
curl http://localhost:8080/ready
# Expected: {"status":"ready","checks":{"kafka":"ok","redis":"ok","models":"ok"}}

# Test metrics
curl http://localhost:8080/metrics
# Expected: Prometheus metrics (detection_count, model_inference_latency, etc.)
```

### 3. Check Kafka Connectivity
```bash
# Exec into AI pod
kubectl exec -it -n cybermesh deployment/ai-service -- /bin/bash

# Test Kafka connection (from inside pod)
nc -zv pkc-ldvr1.asia-southeast1.gcp.confluent.cloud 9092
# Expected: Connection succeeded

# Test Redis
nc -zv merry-satyr-14777.upstash.io 6379
# Expected: Connection succeeded
```

### 4. Check Signing Key Mount
```bash
# Verify key file exists in pod
kubectl exec -n cybermesh deployment/ai-service -- ls -lh /app/keys/
# Expected: signing_key.pem (read-only, 1001:1001)

# Verify key content (first line)
kubectl exec -n cybermesh deployment/ai-service -- head -1 /app/keys/signing_key.pem
# Expected: -----BEGIN PRIVATE KEY-----
```

### 5. Check Resource Usage
```bash
# Get pod metrics (requires metrics-server)
kubectl top pod -n cybermesh -l app=ai-service

# Expected: 
# NAME                          CPU(cores)   MEMORY(bytes)
# ai-service-xxxxxxxxxx-xxxxx   200m         1800Mi
```

### 6. View All Resources
```bash
# See complete cluster state
kubectl get all -n cybermesh

# Expected output:
# 5 validator pods (StatefulSet)
# 1 ai-service pod (Deployment)
# 3 services (validator-api, validator-headless, ai-service)
```

---

## Scaling AI Service

### Scale to 2 Replicas (for HA):
```bash
kubectl scale deployment ai-service -n cybermesh --replicas=2

# Watch scale-up
kubectl get pods -n cybermesh -l app=ai-service -w
```

### Scale to 3 Replicas:
```bash
kubectl scale deployment ai-service -n cybermesh --replicas=3
```

**Notes:**
- AI service is stateless, scales horizontally easily
- Each replica gets its own pod on available nodes
- All replicas share same Kafka consumer group (load balancing)

---

## Troubleshooting

### Pod Not Starting
```bash
# Check pod events
kubectl describe pod -n cybermesh -l app=ai-service

# Common issues:
# - ImagePullBackOff: Image not pushed to registry
# - CrashLoopBackOff: Check logs for errors
# - Pending: Insufficient resources on nodes
```

### Signing Key Not Found
```bash
# Verify secret exists
kubectl get secret ai-service-secret -n cybermesh

# Check secret content (base64 encoded)
kubectl get secret ai-service-secret -n cybermesh -o yaml

# Re-create secret if needed
kubectl delete secret ai-service-secret -n cybermesh
kubectl apply -f k8s/ai-service-secret.yaml
```

### Kafka Connection Failed
```bash
# Check Kafka credentials in backend secret
kubectl get secret cybermesh-secrets -n cybermesh -o json | jq -r '.data.KAFKA_SASL_USERNAME' | base64 -d
kubectl get secret cybermesh-secrets -n cybermesh -o json | jq -r '.data.KAFKA_SASL_PASSWORD' | base64 -d

# Verify from pod
kubectl exec -n cybermesh deployment/ai-service -- env | grep KAFKA
```

### Models Not Loading
```bash
# Check if models exist in image
kubectl exec -n cybermesh deployment/ai-service -- ls -lh /app/data/models/

# Expected:
# ddos_lgbm_v1.0.0.pkl
# malware_lgbm_v1.0.0.pkl
# anomaly_lgbm_v1.0.0.pkl
```

### High Memory Usage
```bash
# Check pod memory
kubectl top pod -n cybermesh -l app=ai-service

# If over 3.5GB, increase limits in deployment.yaml:
# resources.limits.memory: "6Gi"
```

---

## OCI Object Storage Setup (Future)

### When Models Uploaded to OCI Bucket:

**1. Create OCI Bucket:**
```bash
oci os bucket create \
  --compartment-id <your-compartment-id> \
  --name cybermesh-models \
  --public-access-type NoPublicAccess
```

**2. Upload Models:**
```bash
oci os object put \
  --bucket-name cybermesh-models \
  --file ai-service/data/models/ddos_lgbm_v1.0.0.pkl \
  --name models/ddos_lgbm_v1.0.0.pkl

oci os object put \
  --bucket-name cybermesh-models \
  --file ai-service/data/models/malware_lgbm_v1.0.0.pkl \
  --name models/malware_lgbm_v1.0.0.pkl

oci os object put \
  --bucket-name cybermesh-models \
  --file ai-service/data/models/anomaly_lgbm_v1.0.0.pkl \
  --name models/anomaly_lgbm_v1.0.0.pkl
```

**3. Check OCI CSI Driver:**
```bash
kubectl get storageclass | grep oci
# Look for: oci-bv (OCI Block Volume)

kubectl get pods -n kube-system | grep csi
# Should see: oci-csi-controller, oci-csi-node
```

**4. Create PVC:**
```bash
kubectl apply -f k8s/ai-oci-storage-pvc.yaml

# Verify
kubectl get pvc oci-models-pvc -n cybermesh
```

**5. Update Deployment:**
```bash
# Edit ai-service-deployment.yaml
# Uncomment lines 84-86:
#   - name: models
#     mountPath: /app/data/models
#     readOnly: true

# Apply updated deployment
kubectl apply -f k8s/ai-service-deployment.yaml
```

**Fallback if CSI not available:**
- Use init container approach (see comments in `ai-oci-storage-pvc.yaml`)
- Downloads models on pod startup from OCI bucket

---

## Cleanup (If Needed)

### Remove AI Service Only:
```bash
kubectl delete deployment ai-service -n cybermesh
kubectl delete service ai-service -n cybermesh
kubectl delete configmap ai-service-config -n cybermesh
kubectl delete secret ai-service-secret -n cybermesh
kubectl delete serviceaccount ai-service-sa -n cybermesh
kubectl delete role ai-service-role -n cybermesh
kubectl delete rolebinding ai-service-rolebinding -n cybermesh
```

### Or Delete All at Once:
```bash
kubectl delete -f k8s/ai-service-service.yaml
kubectl delete -f k8s/ai-service-deployment.yaml
kubectl delete -f k8s/ai-service-secret.yaml
kubectl delete -f k8s/ai-service-configmap.yaml
kubectl delete -f k8s/ai-service-rbac.yaml
```

---

## Summary

**Files Created:**
1. `k8s/ai-service-rbac.yaml` - ServiceAccount + Role + RoleBinding
2. `k8s/ai-service-configmap.yaml` - All AI config from .env
3. `k8s/ai-service-secret.yaml` - AI signing key (Ed25519)
4. `k8s/ai-service-deployment.yaml` - 1 replica, 2-4GB RAM, probes on 8080
5. `k8s/ai-service-service.yaml` - ClusterIP, ports 8080/10000
6. `k8s/ai-oci-storage-pvc.yaml` - Placeholder for OCI models

**Deployment Order:**
```bash
kubectl apply -f k8s/ai-service-rbac.yaml
kubectl apply -f k8s/ai-service-configmap.yaml
kubectl apply -f k8s/ai-service-secret.yaml
kubectl apply -f k8s/ai-service-deployment.yaml
kubectl apply -f k8s/ai-service-service.yaml
```

**Verification:**
```bash
kubectl get pods -n cybermesh
kubectl logs -f -n cybermesh -l app=ai-service
kubectl port-forward -n cybermesh svc/ai-service 8080:8080
curl http://localhost:8080/health
```

**Cluster After Deployment:**
- 5 backend validator pods (existing)
- 1 AI service pod (new)
- 3 services: validator-api, validator-headless, ai-service
- 2-3 GKE/OKE nodes (auto-scaled by cluster)

---

**Ready to deploy? Run the commands above in order!**
