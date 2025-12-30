package state

import (
	"bytes"
	"sort"
)

type KVPair struct {
	Key   []byte
	Value []byte
}

// BuildRoot computes a deterministic Merkle root over key-sorted KV pairs.
// Leaves are LeafHash(key,value); internal nodes are NodeHash(left,right).
func BuildRoot(pairs []KVPair) [32]byte {
	if len(pairs) == 0 {
		return HashBytes(nil)
	}
	cp := make([]KVPair, len(pairs))
	copy(cp, pairs)
	sort.Slice(cp, func(i, j int) bool { return bytes.Compare(cp[i].Key, cp[j].Key) < 0 })

	level := make([][32]byte, len(cp))
	for i := range cp {
		level[i] = LeafHash(cp[i].Key, cp[i].Value)
	}
	for n := len(level); n > 1; {
		nextN := (n + 1) / 2
		next := make([][32]byte, nextN)
		idx := 0
		for i := 0; i < n; i += 2 {
			if i+1 < n {
				next[idx] = NodeHash(level[i], level[i+1])
			} else {
				// carry last up (hash with zero)
				next[idx] = NodeHash(level[i], [32]byte{})
			}
			idx++
		}
		level = next
		n = len(level)
	}
	return level[0]
}
