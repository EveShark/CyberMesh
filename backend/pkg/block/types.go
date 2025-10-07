package block

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"backend/pkg/consensus/types"
	"backend/pkg/state"
)

const domainBlockHeader = "BLOCK_HEADER_V1"

// AppBlock is the canonical block representation used by the backend.
// It implements consensus/types.Block and provides a deterministic header hash.
type AppBlock struct {
	height        uint64
	parentHash    types.BlockHash
	txRoot        types.BlockHash
	stateRootHint types.BlockHash
	proposerID    types.ValidatorID
	timestamp     time.Time
	hash          types.BlockHash
	txCount       int
	txs           []state.Transaction
}

// NewAppBlock constructs a block and computes its header hash deterministically.
func NewAppBlock(height uint64, parent types.BlockHash, txRoot types.BlockHash, stateRootHint types.BlockHash, proposer types.ValidatorID, ts time.Time, txs []state.Transaction) *AppBlock {
	b := &AppBlock{
		height:        height,
		parentHash:    parent,
		txRoot:        txRoot,
		stateRootHint: stateRootHint,
		proposerID:    proposer,
		timestamp:     ts,
		txCount:       len(txs),
		txs:           txs,
	}
	b.hash = b.computeHeaderHash()
	return b
}

// NewAppBlockHeader constructs a block from header fields only (no txs) and a known txCount.
// Use when reconstructing from storage; transactions can be fetched separately.
func NewAppBlockHeader(height uint64, parent types.BlockHash, txRoot types.BlockHash, stateRootHint types.BlockHash, proposer types.ValidatorID, ts time.Time, txCount int) *AppBlock {
    b := &AppBlock{
        height:        height,
        parentHash:    parent,
        txRoot:        txRoot,
        stateRootHint: stateRootHint,
        proposerID:    proposer,
        timestamp:     ts,
        txCount:       txCount,
        txs:           nil,
    }
    b.hash = b.computeHeaderHash()
    return b
}

// computeHeaderHash builds canonical header bytes and hashes with SHA-256.
// Layout: domain||0x00||height(8B)||parent(32B)||tx_root(32B)||state_root_hint(32B)||proposer(32B)||ts(8B)
func (b *AppBlock) computeHeaderHash() types.BlockHash {
	var out types.BlockHash
	// compute timestamp seconds for canonical header
	ts := b.timestamp.Unix()
	buf := make([]byte, 0, len(domainBlockHeader)+1+8+32+32+32+32+8)
	buf = append(buf, domainBlockHeader...)
	buf = append(buf, 0x00)
	var h [8]byte
	binary.BigEndian.PutUint64(h[:], b.height)
	buf = append(buf, h[:]...)
	buf = append(buf, b.parentHash[:]...)
	buf = append(buf, b.txRoot[:]...)
	buf = append(buf, b.stateRootHint[:]...)
	buf = append(buf, b.proposerID[:]...)
	var tt [8]byte
	binary.BigEndian.PutUint64(tt[:], uint64(ts))
	buf = append(buf, tt[:]...)
	sum := sha256.Sum256(buf)
	copy(out[:], sum[:])
	return out
}

// Block interface implementation
func (b *AppBlock) GetHeight() uint64              { return b.height }
func (b *AppBlock) GetHash() types.BlockHash       { return b.hash }
func (b *AppBlock) GetParentHash() types.BlockHash { return b.parentHash }
func (b *AppBlock) GetTransactionCount() int       { return b.txCount }
func (b *AppBlock) GetTimestamp() time.Time        { return b.timestamp }

// Accessors for header components
func (b *AppBlock) TxRoot() types.BlockHash           { return b.txRoot }
func (b *AppBlock) StateRootHint() types.BlockHash    { return b.stateRootHint }
func (b *AppBlock) Proposer() types.ValidatorID       { return b.proposerID }
func (b *AppBlock) Transactions() []state.Transaction { return b.txs }
