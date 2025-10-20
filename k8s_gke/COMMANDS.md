# CyberMesh Kubernetes - Essential Commands

Quick reference for managing and debugging the CyberMesh consensus backend on GKE.

---

## 1. Cluster & Context

```bash
# Check current cluster
gcloud config get-value project
kubectl config current-context

# Switch to GKE cluster
gcloud container clusters get-credentials cybermesh-cluster \
  --region=asia-southeast1 \
  --project=cybermesh-474414

# List all contexts
kubectl config get-contexts
```

---

## 2. Pod Management

### List pods
```bash
# All pods in namespace
kubectl get pods -n cybermesh

# Watch pod status (live updates)
kubectl get pods -n cybermesh -w

# Wide output (shows node, IP)
kubectl get pods -n cybermesh -o wide

# All resources in namespace
kubectl get all -n cybermesh
```

### Pod details
```bash
# Describe specific pod
kubectl describe pod validator-0 -n cybermesh

# Get pod YAML
kubectl get pod validator-0 -n cybermesh -o yaml

# Check pod status
kubectl get pod validator-0 -n cybermesh -o jsonpath='{.status.phase}'
```

---

## 3. Logs - View & Analyze

### Basic log viewing
```bash
# View logs (last 50 lines)
kubectl logs -n cybermesh validator-0 --tail=50

# Follow logs (live stream)
kubectl logs -n cybermesh validator-0 -f

# Last 100 lines with timestamps
kubectl logs -n cybermesh validator-0 --tail=100 --timestamps

# Previous crashed container
kubectl logs -n cybermesh validator-0 --previous
```

### Multi-pod logs
```bash
# All validator pods (last 50 lines each)
kubectl logs -n cybermesh -l component=validator --tail=50

# All pods with app label
kubectl logs -n cybermesh -l app=consensus-backend --tail=100

# Stream all validator logs
kubectl logs -n cybermesh -l component=validator -f
```

### Init container logs
```bash
# View init container (wait-for-db)
kubectl logs -n cybermesh validator-0 -c wait-for-db
```

### View application log files inside pod
```bash
# Tail last 100 lines from log file
kubectl exec -n cybermesh validator-0 -- sh -c "tail -100 /app/logs/application.log"

# View audit logs
kubectl exec -n cybermesh validator-0 -- sh -c "tail -100 /app/logs/audit.log"

# Follow log file in real-time
kubectl exec -n cybermesh validator-0 -- sh -c "tail -f /app/logs/application.log"

# Search for errors in logs
kubectl exec -n cybermesh validator-0 -- sh -c "grep -i error /app/logs/application.log | tail -50"

# Check log rotation status
kubectl exec -n cybermesh validator-0 -- sh -c "ls -lh /app/logs/"
```

---

## 4. Log File Download & Analysis

### Download entire log file
```bash
# Download application log from validator-0
kubectl exec -n cybermesh validator-0 -- sh -c "cat /app/logs/application.log" > validator-0-app.log

# Download audit log
kubectl exec -n cybermesh validator-0 -- sh -c "cat /app/logs/audit.log" > validator-0-audit.log

# Verify download
ls -lh validator-0-app.log

# Preview (first 20 + last 20 lines)
head -20 validator-0-app.log && echo "..." && tail -20 validator-0-app.log
```

### Download from all validators
```bash
# Download logs from all 5 validators
for i in {0..4}; do
  echo "Downloading from validator-$i..."
  kubectl exec -n cybermesh validator-$i -- sh -c "cat /app/logs/application.log" > validator-$i-app.log
done

# Check downloaded files
ls -lh validator-*-app.log
```

### Download compressed logs
```bash
# Download rotated/compressed logs
kubectl exec -n cybermesh validator-0 -- sh -c "cat /app/logs/application.log.1.gz" > validator-0-app.log.1.gz

# Extract and view
gunzip validator-0-app.log.1.gz
head -50 validator-0-app.log.1
```

### Analyze downloaded logs
```bash
# Count errors
grep -c "ERROR" validator-0-app.log

# Find specific error pattern
grep -i "connection refused" validator-0-app.log

# Extract timestamps for analysis
grep "ERROR" validator-0-app.log | cut -d' ' -f1-2

# Get unique error types
grep "ERROR" validator-0-app.log | awk '{print $NF}' | sort | uniq -c

# View logs in specific time range (if JSON formatted)
jq 'select(.timestamp > "2025-10-07T20:00:00")' validator-0-app.log
```

---

## 5. Execute Commands in Pods

### Interactive shell
```bash
# Open shell in validator-0
kubectl exec -n cybermesh validator-0 -it -- /bin/sh

# Inside pod, useful commands:
# - ps aux                    # Running processes
# - df -h                     # Disk usage
# - netstat -tulpn           # Network connections
# - env                       # Environment variables
# - curl localhost:9441/api/v1/health  # Test local API
```

### One-off commands
```bash
# Check environment variables
kubectl exec -n cybermesh validator-0 -- env | grep NODE_ID

# Test database connectivity
kubectl exec -n cybermesh validator-0 -- sh -c "nc -zv cybermesh-threats-8958.jxf.gcp-asia-southeast1.cockroachlabs.cloud 26257"

# Check process status
kubectl exec -n cybermesh validator-0 -- ps aux

# View mounted secrets
kubectl exec -n cybermesh validator-0 -- ls -la /app/keys/

# Check certificates
kubectl exec -n cybermesh validator-0 -- ls -la /app/certs/
```

### API testing from inside pod
```bash
# Test health endpoint (localhost)
kubectl exec -n cybermesh validator-0 -- sh -c "curl -s http://localhost:9441/api/v1/health"

# Test metrics
kubectl exec -n cybermesh validator-0 -- sh -c "curl -s http://localhost:9100/metrics | head -20"

# Test P2P connectivity to another validator
kubectl exec -n cybermesh validator-0 -- sh -c "nc -zv validator-1.validator-headless 8001"
```

---

## 6. Services & Networking

### Check services
```bash
# List services
kubectl get svc -n cybermesh

# Get LoadBalancer external IP
kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# Describe service
kubectl describe svc validator-api -n cybermesh

# Check endpoints
kubectl get endpoints -n cybermesh
```

### Test external access
```bash
# Get external IP
export LB_IP=$(kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test health endpoint
curl http://$LB_IP:9441/api/v1/health

# Test metrics
curl http://$LB_IP:9100/metrics | head -30

# Detailed curl with headers
curl -v http://$LB_IP:9441/api/v1/health
```

### Port forwarding (local testing)
```bash
# Forward API port to localhost
kubectl port-forward -n cybermesh validator-0 9441:9441

# In another terminal:
curl http://localhost:9441/api/v1/health

# Forward metrics port
kubectl port-forward -n cybermesh svc/validator-api 9100:9100

# Access: http://localhost:9100/metrics
```

---

## 7. Storage & Volumes

### Check PVCs
```bash
# List persistent volume claims
kubectl get pvc -n cybermesh

# Describe PVC
kubectl describe pvc logs-validator-0 -n cybermesh

# Check PVC usage
kubectl exec -n cybermesh validator-0 -- df -h /app/logs

# List all PVs in cluster
kubectl get pv
```

### View volume contents
```bash
# List log files
kubectl exec -n cybermesh validator-0 -- ls -lh /app/logs/

# Check disk usage
kubectl exec -n cybermesh validator-0 -- du -sh /app/logs/*

# Count lines in log files
kubectl exec -n cybermesh validator-0 -- sh -c "wc -l /app/logs/*.log"
```

---

## 8. ConfigMaps & Secrets

### View ConfigMaps
```bash
# List ConfigMaps
kubectl get configmap -n cybermesh

# View ConfigMap data
kubectl get configmap cybermesh-config -n cybermesh -o yaml

# Get specific key
kubectl get configmap cybermesh-config -n cybermesh -o jsonpath='{.data.ENVIRONMENT}'

# Describe ConfigMap
kubectl describe configmap cybermesh-config -n cybermesh
```

### View Secrets
```bash
# List secrets
kubectl get secrets -n cybermesh

# Describe secret (doesn't show values)
kubectl describe secret cybermesh-secrets -n cybermesh

# View secret (base64 encoded)
kubectl get secret cybermesh-secrets -n cybermesh -o yaml

# Decode specific secret key
kubectl get secret cybermesh-secrets -n cybermesh -o jsonpath='{.data.DB_PASSWORD}' | base64 -d
```

---

## 9. Resource Monitoring

### Pod resource usage
```bash
# Current CPU/Memory usage
kubectl top pods -n cybermesh

# Specific pod
kubectl top pod validator-0 -n cybermesh

# Sort by memory
kubectl top pods -n cybermesh --sort-by=memory

# Sort by CPU
kubectl top pods -n cybermesh --sort-by=cpu
```

### Node resource usage
```bash
# All nodes
kubectl top nodes

# Detailed node info
kubectl describe nodes

# Check node allocations
kubectl describe nodes | grep -A 10 "Allocated resources"
```

### Resource limits
```bash
# Check pod resource requests/limits
kubectl get pod validator-0 -n cybermesh -o jsonpath='{.spec.containers[0].resources}'

# View formatted
kubectl describe pod validator-0 -n cybermesh | grep -A 10 "Requests:"
```

---

## 10. Events & Debugging

### View events
```bash
# All events in namespace (sorted by time)
kubectl get events -n cybermesh --sort-by='.lastTimestamp'

# Last 20 events
kubectl get events -n cybermesh --sort-by='.lastTimestamp' | tail -20

# Events for specific pod
kubectl get events -n cybermesh --field-selector involvedObject.name=validator-0

# Watch events live
kubectl get events -n cybermesh -w
```

### Pod troubleshooting
```bash
# Check why pod is not ready
kubectl describe pod validator-0 -n cybermesh | grep -A 20 "Conditions:"

# Check container status
kubectl get pod validator-0 -n cybermesh -o jsonpath='{.status.containerStatuses[0]}'

# Check restart count
kubectl get pod validator-0 -n cybermesh -o jsonpath='{.status.containerStatuses[0].restartCount}'

# Check last termination reason
kubectl get pod validator-0 -n cybermesh -o jsonpath='{.status.containerStatuses[0].lastState}'
```

---

## 11. StatefulSet Operations

### View StatefulSet
```bash
# Get StatefulSet status
kubectl get statefulset -n cybermesh

# Describe StatefulSet
kubectl describe statefulset validator -n cybermesh

# Check rollout status
kubectl rollout status statefulset/validator -n cybermesh
```

### Scale StatefulSet
```bash
# Scale to 7 replicas
kubectl scale statefulset validator -n cybermesh --replicas=7

# Check scaling progress
kubectl get pods -n cybermesh -w

# Scale back to 5
kubectl scale statefulset validator -n cybermesh --replicas=5
```

### Update StatefulSet
```bash
# Update image
kubectl set image statefulset/validator \
  validator=us-central1-docker.pkg.dev/cybermesh-474414/cybermesh-repo/consensus-backend:v1.1.0 \
  -n cybermesh

# Check rollout history
kubectl rollout history statefulset/validator -n cybermesh

# Rollback to previous version
kubectl rollout undo statefulset/validator -n cybermesh
```

---

## 12. Restart & Recovery

### Restart pods
```bash
# Delete pod (StatefulSet recreates it)
kubectl delete pod validator-0 -n cybermesh

# Rolling restart of all pods
kubectl rollout restart statefulset/validator -n cybermesh

# Watch restart progress
kubectl get pods -n cybermesh -w
```

### Force delete stuck pod
```bash
# If pod stuck in Terminating
kubectl delete pod validator-0 -n cybermesh --grace-period=0 --force
```

---

## 13. Quick Health Checks

### One-liner health check
```bash
# Check all pods are running
kubectl get pods -n cybermesh | grep -c "1/1.*Running"
# Should return: 5

# Quick status overview
kubectl get pods,svc,pvc -n cybermesh

# Check for errors in recent logs
kubectl logs -n cybermesh -l component=validator --tail=100 | grep -i error

# Verify all health endpoints
for i in {0..4}; do
  echo "validator-$i:"
  kubectl exec -n cybermesh validator-$i -- curl -s http://localhost:9441/api/v1/health
done
```

### External health check
```bash
# Get LoadBalancer IP and test
export LB_IP=$(kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Testing: http://$LB_IP:9441/api/v1/health"
curl -s http://$LB_IP:9441/api/v1/health | jq .
```

---

## 14. Cleanup & Maintenance

### Restart deployment
```bash
# Restart all validators
kubectl rollout restart statefulset/validator -n cybermesh
```

### Clean up old logs manually
```bash
# Remove old compressed logs
kubectl exec -n cybermesh validator-0 -- sh -c "rm -f /app/logs/*.gz"

# Check space after cleanup
kubectl exec -n cybermesh validator-0 -- df -h /app/logs
```

### Delete and redeploy
```bash
# Delete StatefulSet (keeps PVCs)
kubectl delete statefulset validator -n cybermesh

# Redeploy
kubectl apply -f statefulset.yaml

# Delete everything including PVCs
kubectl delete namespace cybermesh
```

---

## 15. Bulk Operations

### Execute command on all validators
```bash
# Check NODE_ID on all pods
for i in {0..4}; do
  echo "=== validator-$i ==="
  kubectl exec -n cybermesh validator-$i -- sh -c "echo \$NODE_ID"
done

# Check disk usage on all pods
for i in {0..4}; do
  echo "=== validator-$i ==="
  kubectl exec -n cybermesh validator-$i -- df -h /app/logs
done

# Tail logs from all validators
for i in {0..4}; do
  echo "=== validator-$i ==="
  kubectl logs -n cybermesh validator-$i --tail=10
  echo ""
done
```

### Download logs from all pods
```bash
# Create logs directory
mkdir -p cybermesh-logs-$(date +%Y%m%d-%H%M)
cd cybermesh-logs-$(date +%Y%m%d-%H%M)

# Download from all validators
for i in {0..4}; do
  echo "Downloading from validator-$i..."
  kubectl exec -n cybermesh validator-$i -- sh -c "cat /app/logs/application.log" > validator-$i-app.log
  kubectl exec -n cybermesh validator-$i -- sh -c "cat /app/logs/audit.log" > validator-$i-audit.log 2>/dev/null || true
done

# Create summary
ls -lh *.log
wc -l *.log
```

---

## 16. Advanced Log Analysis

### Parse JSON logs
```bash
# If logs are JSON formatted
kubectl logs -n cybermesh validator-0 --tail=100 | jq .

# Filter by log level
kubectl logs -n cybermesh validator-0 --tail=1000 | jq 'select(.level=="ERROR")'

# Extract specific fields
kubectl logs -n cybermesh validator-0 --tail=1000 | jq '{time: .timestamp, level: .level, msg: .message}'
```

### Search patterns across all pods
```bash
# Search for "consensus" in all validator logs
kubectl logs -n cybermesh -l component=validator --tail=500 | grep -i consensus

# Count occurrences of pattern
kubectl logs -n cybermesh -l component=validator --tail=1000 | grep -c "quorum"

# Find errors with context (3 lines before/after)
kubectl logs -n cybermesh validator-0 --tail=1000 | grep -B 3 -A 3 ERROR
```

### Time-based log filtering
```bash
# Download logs and filter by date
kubectl exec -n cybermesh validator-0 -- sh -c "cat /app/logs/application.log" | \
  grep "2025-10-07T20:" | \
  tail -100

# Extract logs from last hour
kubectl exec -n cybermesh validator-0 -- sh -c "
  CUTOFF=\$(date -u -d '1 hour ago' +%Y-%m-%dT%H)
  grep \"\$CUTOFF\" /app/logs/application.log
"
```

---

## 17. Monitoring & Alerts

### Check metrics
```bash
# Get metrics endpoint
export LB_IP=$(kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Fetch metrics
curl -s http://$LB_IP:9100/metrics > metrics.txt

# Analyze metrics
grep "^consensus_" metrics.txt
grep "^go_" metrics.txt | head -10
```

### Watch resource usage
```bash
# Continuous monitoring (update every 2s)
watch -n 2 'kubectl top pods -n cybermesh'

# Monitor specific pod
watch -n 5 'kubectl top pod validator-0 -n cybermesh'
```

---

## Quick Reference Card

```bash
# Essential commands
kubectl get pods -n cybermesh                           # List pods
kubectl logs -n cybermesh validator-0 -f                # Stream logs
kubectl exec -n cybermesh validator-0 -it -- /bin/sh   # Shell access
kubectl describe pod validator-0 -n cybermesh           # Pod details
kubectl get events -n cybermesh --sort-by='.lastTimestamp' | tail -20  # Recent events

# Log file operations
kubectl exec -n cybermesh validator-0 -- sh -c "tail -100 /app/logs/application.log"  # View logs
kubectl exec -n cybermesh validator-0 -- sh -c "cat /app/logs/application.log" > app.log  # Download

# Health checks
export LB_IP=$(kubectl get svc validator-api -n cybermesh -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl http://$LB_IP:9441/api/v1/health                  # External health check

# Monitoring
kubectl top pods -n cybermesh                           # Resource usage
kubectl get all -n cybermesh                            # All resources
```

---

**Namespace:** `cybermesh`
**App Label:** `app=consensus-backend`
**Component Label:** `component=validator`
**LoadBalancer IP:** `34.143.137.254`
