package state

import (
	"bytes"
	"sort"
)

// ListVersions returns existing versions in ascending order.
func (m *MemStore) ListVersions() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]uint64, 0, len(m.snaps))
	for v := range m.snaps {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// VerifyRoot recomputes the merkle root of a version and matches stored root.
func (m *MemStore) VerifyRoot(version uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.snaps[version]
	if !ok {
		return false
	}
	pairs := make([]KVPair, 0, len(snap))
	for k, v := range snap {
		pairs = append(pairs, KVPair{Key: []byte(k), Value: v})
	}
	sort.Slice(pairs, func(i, j int) bool { return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0 })
	root := BuildRoot(pairs)
	r, ok := m.roots[version]
	return ok && bytes.Equal(root[:], r[:])
}

// PruneRetain keeps the most recent retain versions (including latest) and drops older ones.
func (m *MemStore) PruneRetain(retain uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	versions := make([]uint64, 0, len(m.snaps))
	for v := range m.snaps {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	if retain == 0 || uint64(len(versions)) <= retain {
		return
	}
	cutoffIdx := len(versions) - int(retain)
	for i := 0; i < cutoffIdx; i++ {
		v := versions[i]
		if v == m.latest {
			continue
		}
		delete(m.snaps, v)
		delete(m.roots, v)
	}
}

// ExportSnapshot returns a sorted copy of KVs for a version.
func (m *MemStore) ExportSnapshot(version uint64) ([]KVPair, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.snaps[version]
	if !ok {
		return nil, false
	}
	pairs := make([]KVPair, 0, len(snap))
	for k, v := range snap {
		pairs = append(pairs, KVPair{Key: []byte(k), Value: append([]byte(nil), v...)})
	}
	sort.Slice(pairs, func(i, j int) bool { return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0 })
	return pairs, true
}
