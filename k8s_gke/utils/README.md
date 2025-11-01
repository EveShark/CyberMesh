# Utility Manifests

This folder contains utility and maintenance manifests that are not part of the core deployment:

- **clear-db-job.yaml** - Job to clear CockroachDB tables
- **verify-db-job.yaml** - Job to query and verify database contents
- **delete-genesis-job.yaml** - Job to delete genesis certificates
- **statefulset-local.yaml** - Local development StatefulSet (not for GKE)
- **cybermesh-configmap-backup.yaml** - Backup of ConfigMap
- **secret.yaml.template** - Template for creating secrets
