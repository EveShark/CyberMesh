#!/bin/bash
# Dry-run validation script for Genesis Persistence deployment manifests
# This validates all manifests without applying them to the cluster

set -e

echo "========================================="
echo "Genesis Persistence Manifest Dry-Run"
echo "========================================="
echo ""

NAMESPACE="cybermesh"
FAILED=0

# Function to dry-run a manifest
dry_run() {
    local file=$1
    local name=$(basename "$file")
    
    echo "🔍 Validating: $name"
    if kubectl apply -f "$file" --dry-run=client --namespace=$NAMESPACE > /dev/null 2>&1; then
        echo "   ✅ Valid"
    else
        echo "   ❌ FAILED"
        kubectl apply -f "$file" --dry-run=client --namespace=$NAMESPACE
        FAILED=$((FAILED + 1))
    fi
    echo ""
}

# 1. Migration ConfigMap
dry_run "k8s_gke/utils/migration-sql-configmap.yaml"

# 2. Migration Job
dry_run "k8s_gke/utils/migrate-db-job.yaml"

# 3. Verify DB Job
dry_run "k8s_gke/utils/verify-db-job.yaml"

# 4. Clear DB Job
dry_run "k8s_gke/utils/clear-db-job.yaml"

# 5. Delete Genesis Job
dry_run "k8s_gke/utils/delete-genesis-job.yaml"

# 6. Updated ConfigMap
dry_run "k8s_gke/configmap.yaml"

# 7. StatefulSet (to verify it can scale)
dry_run "k8s_gke/statefulset.yaml"

echo "========================================="
if [ $FAILED -eq 0 ]; then
    echo "✅ All manifests are valid!"
    echo "Ready to proceed with deployment."
else
    echo "❌ $FAILED manifest(s) failed validation"
    echo "Fix errors before deploying."
    exit 1
fi
echo "========================================="
