# CyberMesh Kubernetes Deployment

## Prerequisites

1. **Kubernetes Cluster** (one of):
   - Docker Desktop with Kubernetes enabled
   - Minikube
   - Kind
   - Cloud provider (GKE, EKS, AKS)

2. **kubectl** installed and configured

3. **Docker** installed (to build images)

4. **Base64 encoding tool** (for secrets)

---

## Step 1: Prepare Secrets

### A. Base64 Encode Validator Private Keys

```bash
# From CyberMesh root directory
cd keys

# Encode each validator key
cat validator_1_private.pem | base64 -w 0 > validator_1_base64.txt
cat validator_2_private.pem | base64 -w 0 > validator_2_base64.txt
cat validator_3_private.pem | base64 -w 0 > validator_3_base64.txt
cat validator_4_private.pem | base64 -w 0 > validator_4_base64.txt
```

**Windows PowerShell:**
```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes(".\keys\validator_1_private.pem"))
[Convert]::ToBase64String([IO.File]::ReadAllBytes(".\keys\validator_2_private.pem"))
[Convert]::ToBase64String([IO.File]::ReadAllBytes(".\keys\validator_3_private.pem"))
[Convert]::ToBase64String([IO.File]::ReadAllBytes(".\keys\validator_4_private.pem"))
```

### B. Extract Validator Public Keys (Hex)

From your `.env` file, extract lines:
```bash
grep "VALIDATOR_.*_PUBKEY_HEX" .env
```

### C. Update secret.yaml

Edit `k8s/secret.yaml` and replace:
- `PLACEHOLDER_BASE64_KEY_1` → `PLACEHOLDER_BASE64_KEY_4` with base64 encoded keys
- `PLACEHOLDER_HEX_PUBKEY_1` → `PLACEHOLDER_HEX_PUBKEY_4` with hex public keys

---

## Step 2: Build Docker Image

```bash
# From CyberMesh root directory
docker build -t cybermesh/consensus-backend:latest .

# Verify image size (should be <100MB)
docker images | grep cybermesh
```

**For Minikube/Kind (load image into cluster):**
```bash
# Minikube
minikube image load cybermesh/consensus-backend:latest

# Kind
kind load docker-image cybermesh/consensus-backend:latest
```

---

## Step 3: Deploy to Kubernetes

### Apply manifests in order:

```bash
cd k8s

# 1. Namespace
kubectl apply -f namespace.yaml

# 2. RBAC (Service Account, Role, RoleBinding)
kubectl apply -f rbac.yaml

# 3. ConfigMaps
kubectl apply -f configmap.yaml
kubectl apply -f configmap-db-cert.yaml

# 4. Secrets (AFTER you've updated them!)
kubectl apply -f secret.yaml

# 5. Services
kubectl apply -f service-headless.yaml
kubectl apply -f service.yaml

# 6. PodDisruptionBudget
kubectl apply -f pdb.yaml

# 7. StatefulSet (deploys 4 validator nodes)
kubectl apply -f statefulset.yaml
```

**Or apply all at once:**
```bash
kubectl apply -f .
```

---

## Step 4: Verify Deployment

### Check pods are running:
```bash
kubectl get pods -n cybermesh -w

# Expected output:
# validator-0   1/1     Running   0          2m
# validator-1   1/1     Running   0          2m
# validator-2   1/1     Running   0          2m
# validator-3   1/1     Running   0          2m
```

### Check logs:
```bash
# All pods
kubectl logs -n cybermesh -l component=validator --tail=50

# Specific pod
kubectl logs -n cybermesh validator-0 -f
```

### Test health endpoints:
```bash
# Port-forward to validator-0
kubectl port-forward -n cybermesh validator-0 9441:9441

# In another terminal
curl http://localhost:9441/api/v1/health
```

### Test consensus:
```bash
# Check if nodes can see each other
kubectl exec -n cybermesh validator-0 -- /bin/sh -c "curl http://validator-1.validator-headless:9441/api/v1/health"
```

---

## Step 5: Access API

### Via LoadBalancer:
```bash
# Get external IP
kubectl get svc -n cybermesh validator-api

# Access API
curl http://<EXTERNAL-IP>:9441/api/v1/health
```

### Via Port Forward (development):
```bash
kubectl port-forward -n cybermesh svc/validator-api 9441:9441

# Access locally
curl http://localhost:9441/api/v1/health
curl http://localhost:9441/api/v1/metrics
```

---

## Scaling

### Scale to more validators:
```bash
# Scale to 7 nodes (for f=2 Byzantine fault tolerance)
kubectl scale statefulset validator -n cybermesh --replicas=7
```

---

## Troubleshooting

### Pods not starting?
```bash
# Check events
kubectl get events -n cybermesh --sort-by='.lastTimestamp'

# Describe pod
kubectl describe pod -n cybermesh validator-0
```

### Database connection issues?
```bash
# Test DB connectivity from pod
kubectl exec -n cybermesh validator-0 -- nc -zv cybermesh-threats-8958.jxf.gcp-asia-southeast1.cockroachlabs.cloud 26257
```

### P2P discovery not working?
```bash
# Check DNS resolution
kubectl exec -n cybermesh validator-0 -- nslookup validator-headless

# Should return all pod IPs
```

### Check resource usage:
```bash
kubectl top pods -n cybermesh
```

---

## Log Management

### Dual Logging Strategy

**Application logs** → stdout (JSON format) + `/app/logs/application.log`  
**Audit logs** → `/app/logs/audit.log`

This dual approach ensures:
- ✅ Real-time log streaming via `kubectl logs`
- ✅ File access when pods hang/deadlock
- ✅ Log persistence across restarts (PVC storage)

### View Logs

**Via kubectl (stdout):**
```bash
# Follow logs for validator-0
kubectl logs -n cybermesh validator-0 -f

# Last 100 lines
kubectl logs -n cybermesh validator-0 --tail=100

# All validators
kubectl logs -n cybermesh -l component=validator --all-containers=true
```

**Via file (debugging stuck pods):**
```bash
# Exec into pod
kubectl exec -n cybermesh validator-0 -it -- /bin/sh

# View application logs
tail -f /app/logs/application.log

# View audit logs
tail -f /app/logs/audit.log

# Check rotated logs
ls -lh /app/logs/
```

### Log Configuration

Configured in `configmap.yaml`:

```yaml
LOG_LEVEL: "info"                       # debug, info, warn, error
LOG_FILE_PATH: "/app/logs/application.log"
SERVICE_NAME: "cybermesh"
LOG_MAX_SIZE: "100"                     # MB per file
LOG_MAX_BACKUPS: "10"                   # Keep 10 rotated files
LOG_MAX_AGE: "30"                       # Days to retain
LOG_COMPRESS: "true"                    # Compress rotated logs
```

### Log Storage

Each validator node has a **10Gi PersistentVolume** for logs:

```bash
# Check PVCs
kubectl get pvc -n cybermesh

# Should show: logs-validator-0, logs-validator-1, etc.
```

**Storage class:** `standard-rwo` (GKE standard persistent disk)

### Log Rotation

Automatic rotation managed by lumberjack:
- **Max size:** 100MB per file
- **Backups:** 10 old files kept
- **Compression:** Old logs gzipped
- **Retention:** 30 days

Example rotated logs:
```
/app/logs/
  application.log          # Current
  application.log.1.gz     # Yesterday
  application.log.2.gz     # 2 days ago
  ...
  audit.log
  audit.log.1.gz
```

### Production Log Aggregation

For production, integrate with centralized logging:

**Fluentd/Fluent Bit:**
```bash
# Install Fluent Bit DaemonSet
kubectl apply -f https://raw.githubusercontent.com/fluent/fluent-bit-kubernetes-logging/master/output/elasticsearch/fluent-bit-ds.yaml
```

**Loki + Grafana:**
```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm install loki grafana/loki-stack -n monitoring
```

**Cloud Provider Solutions:**
- **GKE:** Cloud Logging (automatic)
- **EKS:** CloudWatch Container Insights
- **AKS:** Azure Monitor for containers

---

## Cleanup

```bash
# Delete everything
kubectl delete namespace cybermesh

# Or delete individually
kubectl delete -f k8s/
```

---

## Production Checklist

- [ ] Updated all secrets in `secret.yaml`
- [ ] Verified image size <100MB
- [ ] Tested health endpoints
- [ ] Verified Byzantine fault tolerance (min 3/4 nodes)
- [ ] Configured monitoring (Prometheus/Grafana)
- [ ] Set up log aggregation (ELK/Loki)
- [ ] Resource limits tuned for workload
- [ ] PodDisruptionBudget ensures quorum
- [ ] Network policies configured
- [ ] TLS enabled for production

---

## Next Steps

1. **Monitoring**: Deploy Prometheus + Grafana
2. **Logging**: Set up centralized logging
3. **Alerting**: Configure alerts for quorum loss
4. **Backups**: Database backup strategy
5. **CI/CD**: Automated deployment pipeline
