# Node Labels Solution - GKE Autoscaling Issue

## Problem

When scaling workloads to 0, GKE **terminates old nodes** to save costs. When scaling back up, GKE **creates new nodes with different names**. 

**Our custom node labels are lost** because they were applied to individual nodes, not persisted at the pool level.

## Manifestation

After scale-down and scale-up:
- Pods stuck in `Pending` state
- Error: `node(s) didn't match Pod's node affinity/selector`
- Reason: New nodes don't have `workload-type` labels

## Current Workaround (Manual)

After every scale-up or node recreation, manually re-label nodes:

```bash
# Get current nodes
kubectl get nodes

# Label each node based on intended workload
kubectl label nodes <node-name-1> workload-type=stateless --overwrite
kubectl label nodes <node-name-2> workload-type=validators --overwrite
kubectl label nodes <node-name-3> workload-type=validators --overwrite
```

## Permanent Solution Options

### Option 1: Create Separate Node Pools (RECOMMENDED)

**Best for production** - Labels automatically apply to all nodes in each pool.

```bash
# Delete existing default pool
gcloud container node-pools delete default-pool \
  --cluster=cybermesh \
  --region=asia-southeast1

# Create stateless workload pool (1 node)
gcloud container node-pools create stateless-pool \
  --cluster=cybermesh \
  --region=asia-southeast1 \
  --machine-type=e2-standard-2 \
  --num-nodes=1 \
  --node-labels=workload-type=stateless \
  --enable-autoscaling \
  --min-nodes=0 \
  --max-nodes=1

# Create validator pool (2 nodes)
gcloud container node-pools create validator-pool \
  --cluster=cybermesh \
  --region=asia-southeast1 \
  --machine-type=e2-standard-2 \
  --num-nodes=2 \
  --node-labels=workload-type=validators \
  --enable-autoscaling \
  --min-nodes=0 \
  --max-nodes=3
```

**Benefits:**
- ✅ Labels persist automatically on ALL new nodes
- ✅ Survives scale-to-0 operations
- ✅ Better resource isolation
- ✅ Different autoscaling policies per workload type
- ✅ Can use different machine types per pool

**Drawbacks:**
- More complex cluster configuration
- Requires cluster downtime to implement

### Option 2: Use Node Pool Metadata Labels

**Simpler but less flexible** - Apply same label to entire pool.

```bash
gcloud container node-pools update default-pool \
  --node-labels=workload-type=mixed \
  --cluster=cybermesh \
  --region=asia-southeast1
```

**Benefits:**
- ✅ Labels persist automatically
- ✅ No cluster downtime
- ✅ Simple implementation

**Drawbacks:**
- ❌ All nodes get SAME label (can't differentiate stateless vs validators)
- ❌ Defeats purpose of pod distribution strategy

### Option 3: GKE Node Auto-Provisioning with Taints/Tolerations

Use node taints instead of labels:

```yaml
# In pod spec
tolerations:
- key: "workload-type"
  operator: "Equal"
  value: "stateless"
  effect: "NoSchedule"
```

**Benefits:**
- ✅ More robust than labels
- ✅ Prevents wrong pods from scheduling

**Drawbacks:**
- ❌ Requires rewriting all manifests
- ❌ More complex than labels

## Recommendation

**For CyberMesh:** Use **Option 1 (Separate Node Pools)**

**Why:**
1. Production-grade solution
2. Clear workload separation
3. Better cost control (can scale validator pool independently)
4. Survives all scale operations
5. Future-proof for cluster upgrades

## Implementation Steps

1. **Backup current state:**
   ```bash
   kubectl get all -n cybermesh -o yaml > cybermesh-backup.yaml
   ```

2. **Create new node pools** (see Option 1 commands above)

3. **Wait for pools to be ready:**
   ```bash
   kubectl get nodes --show-labels
   ```

4. **Delete old default pool:**
   ```bash
   gcloud container node-pools delete default-pool \
     --cluster=cybermesh \
     --region=asia-southeast1
   ```

5. **Verify pods reschedule:**
   ```bash
   kubectl get pods -n cybermesh -o wide
   ```

## Current Manifest Changes

All manifests now include `nodeSelector`:

- ✅ `statefulset.yaml` (validators) → `workload-type: validators`
- ✅ `postgres-statefulset.yaml` → `workload-type: stateless`
- ✅ `ai-service-deployment.yaml` → `workload-type: stateless`
- ✅ `frontend-deployment.yaml` → `workload-type: stateless`
- ✅ `utils/cleanup-telemetry-cronjob.yaml` → `workload-type: stateless`
- ✅ `utils/clear-db-job.yaml` → `workload-type: stateless`
- ✅ `utils/verify-db-job.yaml` → `workload-type: stateless`
- ✅ `utils/delete-genesis-job.yaml` → `workload-type: stateless`

**These work correctly ONLY if nodes have the proper labels!**

## Testing

After implementing permanent solution:

```bash
# 1. Scale everything to 0
kubectl scale statefulset validator postgres --replicas=0 -n cybermesh
kubectl scale deployment ai-service frontend --replicas=0 -n cybermesh

# 2. Wait for nodes to terminate (GKE autoscaling)
kubectl get nodes -w

# 3. Scale back up
kubectl scale statefulset validator --replicas=5 -n cybermesh
kubectl scale statefulset postgres --replicas=1 -n cybermesh
kubectl scale deployment ai-service frontend --replicas=1 -n cybermesh

# 4. Verify pods schedule WITHOUT manual intervention
kubectl get pods -n cybermesh -o wide

# 5. Check node labels persist
kubectl get nodes --show-labels
```

**If successful:** Pods schedule immediately, no manual labeling needed! ✅

---

**Last Updated:** 2025-10-31  
**Status:** Manual workaround active, permanent solution pending implementation
