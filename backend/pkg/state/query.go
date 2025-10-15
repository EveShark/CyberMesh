package state

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"
)

var ErrNotFound = errors.New("not found")

// PolicyLatest returns the latest policy content hash and payload at version.
func (m *MemStore) PolicyLatest(version uint64) ([32]byte, []byte, error) {
	var z [32]byte
	v, ok := m.Get(version, keyPolicyHead)
	if !ok || len(v) != 32 {
		return z, nil, ErrNotFound
	}
	copy(z[:], v)
	p, ok := m.Get(version, keyPolicy(z))
	if !ok {
		return z, nil, ErrNotFound
	}
	return z, p, nil
}

// EventByHash returns event payload by content hash at version.
func (m *MemStore) EventByHash(version uint64, h [32]byte) ([]byte, error) {
	v, ok := m.Get(version, keyEvent(h))
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

// EvidenceByHash returns evidence payload by content hash at version.
func (m *MemStore) EvidenceByHash(version uint64, h [32]byte) ([]byte, error) {
	v, ok := m.Get(version, keyEvidence(h))
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

// ProducerStats returns last timestamp and count for a producer at version.
func (m *MemStore) ProducerStats(version uint64, producerID []byte) (int64, uint64, error) {
	ltB, ok := m.Get(version, keyProducerLast(producerID))
	if !ok || len(ltB) != 8 {
		return 0, 0, ErrNotFound
	}
	last := int64(binary.BigEndian.Uint64(ltB))
	cntB, ok := m.Get(version, keyProducerCnt(producerID))
	var cnt uint64
	if ok && len(cntB) == 8 {
		cnt = binary.BigEndian.Uint64(cntB)
	}
	return last, cnt, nil
}

// NonceMapping returns content hash recorded for a producer nonce at version.
func (m *MemStore) NonceMapping(version uint64, producerID, nonce []byte) ([32]byte, error) {
	var z [32]byte
	v, ok := m.Get(version, keyNonce(producerID, nonce))
	if !ok || len(v) != 32 {
		return z, ErrNotFound
	}
	copy(z[:], v)
	return z, nil
}

// MerkleProof represents a simple proof from leaf to root.
type MerkleProof struct {
	Leaf     [32]byte
	Siblings [][32]byte
	Left     []bool
}

// Proof builds a proof for a key at version. Returns proof and root.
func (m *MemStore) Proof(version uint64, key []byte) (MerkleProof, [32]byte, error) {
	var empty MerkleProof
	snap, ok := m.snaps[version]
	if !ok {
		return empty, [32]byte{}, ErrNotFound
	}
	pairs := make([]KVPair, 0, len(snap))
	for k, v := range snap {
		pairs = append(pairs, KVPair{Key: []byte(k), Value: v})
	}
	sort.Slice(pairs, func(i, j int) bool { return bytes.Compare(pairs[i].Key, pairs[j].Key) < 0 })
	// Build leaf list and locate index
	idx := -1
	leaves := make([][32]byte, len(pairs))
	for i := range pairs {
		leaves[i] = LeafHash(pairs[i].Key, pairs[i].Value)
		if bytes.Equal(pairs[i].Key, key) {
			idx = i
		}
	}
	if idx < 0 {
		return empty, [32]byte{}, ErrNotFound
	}
	proof := MerkleProof{Leaf: leaves[idx]}
	level := leaves
	pos := idx
	for n := len(level); n > 1; {
		// sibling index
		var sib [32]byte
		if pos%2 == 0 { // right sibling
			if pos+1 < n {
				sib = level[pos+1]
			} else {
				sib = [32]byte{}
			}
			proof.Siblings = append(proof.Siblings, sib)
			proof.Left = append(proof.Left, false) // sibling on right
		} else {
			sib = level[pos-1]
			proof.Siblings = append(proof.Siblings, sib)
			proof.Left = append(proof.Left, true) // sibling on left
		}
		// advance to next level
		nextN := (n + 1) / 2
		next := make([][32]byte, nextN)
		for i := 0; i < n; i += 2 {
			if i+1 < n {
				next[i/2] = NodeHash(level[i], level[i+1])
			} else {
				next[i/2] = NodeHash(level[i], [32]byte{})
			}
		}
		level = next
		pos /= 2
		n = len(level)
	}
	root := level[0]
	return proof, root, nil
}

// VerifyProof verifies a proof against a root.
func VerifyProof(root [32]byte, p MerkleProof) bool {
	h := p.Leaf
	for i := range p.Siblings {
		if p.Left[i] {
			h = NodeHash(p.Siblings[i], h)
		} else {
			h = NodeHash(h, p.Siblings[i])
		}
	}
	return bytes.Equal(h[:], root[:])
}
