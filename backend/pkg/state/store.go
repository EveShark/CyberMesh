package state

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

const (
	MaxStateKeySize   = 256
	MaxStateValueSize = 1 << 20
)

var (
	ErrStaleVersion = errors.New("stale version")
	ErrInvalidKey   = errors.New("invalid key")
	ErrInvalidValue = errors.New("invalid value")
)

type StateStore interface {
	Begin(version uint64) (*Txn, error)
	Get(version uint64, key []byte) ([]byte, bool)
	Root(version uint64) ([32]byte, bool)
	Latest() uint64
}

type MemStore struct {
	mu     sync.RWMutex
	snaps  map[uint64]map[string][]byte
	roots  map[uint64][32]byte
	latest uint64
}

func NewMemStore() *MemStore {
	ms := &MemStore{snaps: make(map[uint64]map[string][]byte), roots: make(map[uint64][32]byte)}
	ms.snaps[0] = make(map[string][]byte)
	ms.roots[0] = HashBytes(nil)
	ms.latest = 0
	return ms
}

func (m *MemStore) Latest() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latest
}

func (m *MemStore) Root(version uint64) ([32]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.roots[version]
	return r, ok
}

func (m *MemStore) Begin(version uint64) (*Txn, error) {
	m.mu.RLock()
	latest := m.latest
	_, ok := m.snaps[version]
	m.mu.RUnlock()
	if !ok || version != latest {
		return nil, ErrStaleVersion
	}
	return &Txn{store: m, baseVersion: version, writes: make(map[string][]byte)}, nil
}

func (m *MemStore) Get(version uint64, key []byte) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snap, ok := m.snaps[version]
	if !ok {
		return nil, false
	}
	v, ok := snap[string(key)]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

type Txn struct {
	store       *MemStore
	baseVersion uint64
	writes      map[string][]byte
}

func (t *Txn) Put(key, value []byte) error {
	if len(key) == 0 || len(key) > MaxStateKeySize {
		return ErrInvalidKey
	}
	if len(value) > MaxStateValueSize {
		return ErrInvalidValue
	}
	t.writes[string(key)] = append([]byte(nil), value...)
	return nil
}

func (t *Txn) Delete(key []byte) error {
	if len(key) == 0 || len(key) > MaxStateKeySize {
		return ErrInvalidKey
	}
	t.writes[string(key)] = nil
	return nil
}

func (t *Txn) Get(key []byte) ([]byte, bool) {
	if v, ok := t.writes[string(key)]; ok {
		if v == nil {
			return nil, false
		}
		return append([]byte(nil), v...), true
	}
	return t.store.Get(t.baseVersion, key)
}

func (t *Txn) Commit(newVersion uint64) ([32]byte, error) {
	var zero [32]byte
	m := t.store
	m.mu.Lock()
	defer m.mu.Unlock()
	if newVersion != t.baseVersion+1 || m.latest != t.baseVersion {
		return zero, ErrStaleVersion
	}
	base := m.snaps[t.baseVersion]
	next := make(map[string][]byte, len(base)+len(t.writes))
	for k, v := range base {
		next[k] = append([]byte(nil), v...)
	}
	for k, v := range t.writes {
		if v == nil {
			delete(next, k)
		} else {
			next[k] = append([]byte(nil), v...)
		}
	}
	pairs := make([]KVPair, 0, len(next))
	for k, v := range next {
		pairs = append(pairs, KVPair{Key: []byte(k), Value: v})
	}
	sort.Slice(pairs, func(i, j int) bool { return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0 })
	root := BuildRoot(pairs)
	m.snaps[newVersion] = next
	m.roots[newVersion] = root
	m.latest = newVersion
	return root, nil
}
