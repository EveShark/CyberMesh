#!/bin/bash
# GKE Deployment Script for CyberMesh
# Auto-generated: 20251226_124724
# Project: sunny-vehicle-482107-p5 | Region: us-central1

set -e

echo "=== CyberMesh GKE Deployment ==="
echo "Project: sunny-vehicle-482107-p5"
echo "Region:  us-central1"
echo ""

# 1. Apply Namespace & RBAC
echo "--- 1. Security & Access Control ---"
kubectl apply -f ./namespace.yaml
kubectl apply -f ./rbac.yaml
kubectl apply -f ./ai-service-rbac.yaml
kubectl apply -f ./enforcement-agent-rbac.yaml

# 2. Configs & Secrets
echo "--- 2. Configuration ---"
# Note: Manually create secrets if they don't exist!
kubectl apply -f ./configmap.yaml
kubectl apply -f ./configmap-db-cert.yaml
kubectl apply -f ./ai-service-configmap.yaml
kubectl apply -f ./ai-service-entrypoint-configmap.yaml
kubectl apply -f ./ai-service-secret.yaml
kubectl apply -f ./ai-service-postgres-secret.yaml
if [ -f "./secret.yaml" ]; then kubectl apply -f ./secret.yaml; fi
if [ -f "./backend-trusted-keys-secret.yaml" ]; then kubectl apply -f ./backend-trusted-keys-secret.yaml; fi

# 3. Storage
echo "--- 3. Storage ---"
kubectl apply -f ./ai-service-pvc.yaml
kubectl apply -f ./ai-oci-storage-pvc.yaml

# 4. Services
echo "--- 4. Services ---"
kubectl apply -f ./service.yaml
kubectl apply -f ./service-headless.yaml
kubectl apply -f ./ai-service-service.yaml
kubectl apply -f ./frontend-service.yaml

# 5. Workloads
echo "--- 5. Workloads ---"
kubectl apply -f ./pdb.yaml
kubectl apply -f ./postgres-statefulset.yaml
kubectl apply -f ./statefulset.yaml
kubectl apply -f ./ai-service-deployment.yaml
kubectl apply -f ./frontend-deployment.yaml
kubectl apply -f ./daemonset.yaml

# 6. Jobs
echo "--- 6. Jobs ---"
kubectl apply -f ./utils/migrate-db-job.yaml

echo ""
echo "=== Deployment Applied ==="
echo "Monitor status:"
echo "  kubectl get pods -n cybermesh -w"
