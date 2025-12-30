package block

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

// AppTransactionPayload captures transaction data in a CBOR-friendly format for wire transport.
type AppTransactionPayload struct {
	Type      state.TxType     `cbor:"1,keyasint"`
	Timestamp int64            `cbor:"2,keyasint"`
	Payload   []byte           `cbor:"3,keyasint"`
	Envelope  state.Envelope   `cbor:"4,keyasint"`
	CoC       []state.CoCEntry `cbor:"5,keyasint,omitempty"`
}

// AppBlockPayload is the canonical wire representation of an AppBlock.
type AppBlockPayload struct {
	Height        uint64                  `cbor:"1,keyasint"`
	ParentHash    types.BlockHash         `cbor:"2,keyasint"`
	TxRoot        types.BlockHash         `cbor:"3,keyasint"`
	StateRootHint types.BlockHash         `cbor:"4,keyasint"`
	Proposer      types.ValidatorID       `cbor:"5,keyasint"`
	Timestamp     time.Time               `cbor:"6,keyasint"`
	Transactions  []AppTransactionPayload `cbor:"7,keyasint"`
}

// ToPayload converts an AppBlock into its wire representation.
func (b *AppBlock) ToPayload() (*AppBlockPayload, error) {
	if b == nil {
		return nil, fmt.Errorf("nil block")
	}

	txs := b.Transactions()
	payloadTxs := make([]AppTransactionPayload, len(txs))
	for i, tx := range txs {
		switch t := tx.(type) {
		case *state.EventTx:
			payloadTxs[i] = AppTransactionPayload{
				Type:      state.TxEvent,
				Timestamp: t.Ts,
				Payload:   cloneBytes(t.Data),
				Envelope:  cloneEnvelope(t.Env),
			}
		case *state.EvidenceTx:
			payloadTxs[i] = AppTransactionPayload{
				Type:      state.TxEvidence,
				Timestamp: t.Ts,
				Payload:   cloneBytes(t.Data),
				Envelope:  cloneEnvelope(t.Env),
				CoC:       cloneCoCEntries(t.CoC),
			}
		case *state.PolicyTx:
			payloadTxs[i] = AppTransactionPayload{
				Type:      state.TxPolicy,
				Timestamp: t.Ts,
				Payload:   cloneBytes(t.Data),
				Envelope:  cloneEnvelope(t.Env),
			}
		default:
			return nil, fmt.Errorf("unsupported transaction type %T", tx)
		}
	}

	return &AppBlockPayload{
		Height:        b.height,
		ParentHash:    b.parentHash,
		TxRoot:        b.txRoot,
		StateRootHint: b.stateRootHint,
		Proposer:      b.proposerID,
		Timestamp:     b.timestamp,
		Transactions:  payloadTxs,
	}, nil
}

// ToAppBlock reconstructs an AppBlock from a wire payload.
func (p *AppBlockPayload) ToAppBlock() (*AppBlock, error) {
	if p == nil {
		return nil, fmt.Errorf("nil block payload")
	}

	txs := make([]state.Transaction, len(p.Transactions))
	for i, tx := range p.Transactions {
		env := cloneEnvelope(tx.Envelope)
		switch tx.Type {
		case state.TxEvent:
			txs[i] = &state.EventTx{
				Ts:   tx.Timestamp,
				Data: cloneBytes(tx.Payload),
				Env:  env,
			}
		case state.TxEvidence:
			txs[i] = &state.EvidenceTx{
				Ts:   tx.Timestamp,
				Data: cloneBytes(tx.Payload),
				Env:  env,
				CoC:  cloneCoCEntries(tx.CoC),
			}
		case state.TxPolicy:
			txs[i] = &state.PolicyTx{
				Ts:   tx.Timestamp,
				Data: cloneBytes(tx.Payload),
				Env:  env,
			}
		default:
			return nil, fmt.Errorf("unsupported transaction type %s", tx.Type)
		}
	}

	// Validate transaction root consistency
	computedRoot := computeTxRootForPayload(txs)
	if computedRoot != p.TxRoot {
		return nil, fmt.Errorf("transaction root mismatch: expected %x, got %x", p.TxRoot, computedRoot)
	}

	blk := &AppBlock{
		height:        p.Height,
		parentHash:    p.ParentHash,
		txRoot:        p.TxRoot,
		stateRootHint: p.StateRootHint,
		proposerID:    p.Proposer,
		timestamp:     p.Timestamp,
		txCount:       len(txs),
		txs:           txs,
	}
	blk.hash = blk.computeHeaderHash()
	return blk, nil
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

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

func cloneEnvelope(env state.Envelope) state.Envelope {
	cloned := state.Envelope{
		ProducerID:  cloneBytes(env.ProducerID),
		Nonce:       cloneBytes(env.Nonce),
		ContentHash: env.ContentHash,
		Alg:         env.Alg,
		PubKey:      cloneBytes(env.PubKey),
		Signature:   cloneBytes(env.Signature),
	}
	return cloned
}

func cloneCoCEntries(entries []state.CoCEntry) []state.CoCEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]state.CoCEntry, len(entries))
	for i, e := range entries {
		out[i] = state.CoCEntry{
			RefHash:   e.RefHash,
			ActorID:   cloneBytes(e.ActorID),
			Ts:        e.Ts,
			Signature: cloneBytes(e.Signature),
		}
	}
	return out
}

func computeTxRootForPayload(txs []state.Transaction) types.BlockHash {
	if len(txs) == 0 {
		return types.BlockHash(state.HashBytes(nil))
	}
	pairs := make([]state.KVPair, 0, len(txs))
	for _, tx := range txs {
		h := tx.Envelope().ContentHash
		pairs = append(pairs, state.KVPair{Key: cloneBytes(h[:]), Value: nil})
	}
	root := state.BuildRoot(pairs)
	var out types.BlockHash
	copy(out[:], root[:])
	return out
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
