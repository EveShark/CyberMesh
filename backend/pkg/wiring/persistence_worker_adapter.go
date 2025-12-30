package wiring

import (
	"backend/pkg/consensus/types"
	"backend/pkg/storage/cockroach"
)

func (pw *PersistenceWorker) GetAdapter() cockroach.Adapter {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	return pw.adapter
}

// GetStorageBackend returns the cockroach adapter as a StorageBackend for consensus persistence
func (pw *PersistenceWorker) GetStorageBackend() types.StorageBackend {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	return pw.adapter
}
