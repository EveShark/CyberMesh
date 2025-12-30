package state

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

// Domains for transaction hashing
const (
	DomainEventTx    = "APP_EVENT_V1"
	DomainEvidenceTx = "APP_EVIDENCE_V1"
	DomainPolicyTx   = "APP_POLICY_V1"
)

// Domains for state hashing
const (
	DomainStateLeaf = "STATE_LEAF_V1"
	DomainStateNode = "STATE_NODE_V1"
)

// Size limits (bytes)
const (
	MaxTxPayloadSize  = 1 << 20 // 1 MiB
	MaxSignedEnvelope = 2 << 20 // 2 MiB
	MaxProducerIDSize = 64
	NonceSize         = 16
	MaxAlgSize        = 16
	PubKeyEd25519Size = 32
	SigEd25519Size    = 64
	MaxCoCEntries     = 64
	MaxActorIDSize    = 64
)

var (
	ErrOversize = errors.New("payload too large")
	ErrSkew     = errors.New("timestamp skew too large")
)

// ValidateSize enforces an upper bound on blob size
func ValidateSize(n, max int) error {
	if n < 0 || n > max {
		return ErrOversize
	}
	return nil
}

// CheckSkew verifies the provided unix timestamp is within an allowed skew window
func CheckSkew(unixSeconds int64, now time.Time, skew time.Duration) error {
	ts := time.Unix(unixSeconds, 0)
	if now.Sub(ts) > skew {
		return ErrSkew
	}
	if ts.Sub(now) > skew {
		return ErrSkew
	}
	return nil
}

// CanonicalTxBytes builds canonical bytes for transaction hashing/signing
// Layout: domain||0x00||ts(8B BE)||payload
func CanonicalTxBytes(domain string, ts int64, payload []byte) ([]byte, error) {
	if err := ValidateSize(len(payload), MaxTxPayloadSize); err != nil {
		return nil, err
	}
	// domain + sep + ts + payload
	// domain is small, avoid reallocs with bounded preallocation
	b := make([]byte, 0, len(domain)+1+8+len(payload))
	b = append(b, domain...)
	b = append(b, 0x00)
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], uint64(ts))
	b = append(b, t[:]...)
	b = append(b, payload...)
	return b, nil
}

// HashBytes returns SHA-256 hash of input
func HashBytes(b []byte) [32]byte { return sha256.Sum256(b) }

// ContentHash computes SHA-256 over the raw payload
func ContentHash(payload []byte) [32]byte { return sha256.Sum256(payload) }

// TxHash computes the transaction hash using canonical bytes
func TxHash(domain string, ts int64, payload []byte) ([32]byte, error) {
	var z [32]byte
	b, err := CanonicalTxBytes(domain, ts, payload)
	if err != nil {
		return z, err
	}
	return HashBytes(b), nil
}

// LeafHash hashes a state key/value leaf deterministically
// Layout: DomainStateLeaf||0x00||key_len(4B BE)||key||value_hash(32B)
func LeafHash(key, value []byte) [32]byte {
	vh := sha256.Sum256(value)
	var kl [4]byte
	binary.BigEndian.PutUint32(kl[:], uint32(len(key)))
	buf := make([]byte, 0, len(DomainStateLeaf)+1+4+len(key)+len(vh))
	buf = append(buf, DomainStateLeaf...)
	buf = append(buf, 0x00)
	buf = append(buf, kl[:]...)
	buf = append(buf, key...)
	buf = append(buf, vh[:]...)
	return sha256.Sum256(buf)
}

// NodeHash hashes an internal node from left/right child hashes
// Layout: DomainStateNode||0x00||left(32B)||right(32B)
func NodeHash(left, right [32]byte) [32]byte {
	buf := make([]byte, 0, len(DomainStateNode)+1+32+32)
	buf = append(buf, DomainStateNode...)
	buf = append(buf, 0x00)
	buf = append(buf, left[:]...)
	buf = append(buf, right[:]...)
	return sha256.Sum256(buf)
}

// BuildSignBytes constructs the byte string to be signed by producers
// Layout: domain||0x00||ts(8B)||producer_id_len(2B)||producer_id||nonce(16B)||content_hash(32B)
func BuildSignBytes(domain string, ts int64, producerID []byte, nonce []byte, contentHash [32]byte) ([]byte, error) {
	if len(producerID) == 0 || len(producerID) > MaxProducerIDSize {
		return nil, errors.New("invalid producer id size")
	}
	if len(nonce) != NonceSize {
		return nil, errors.New("invalid nonce size")
	}
	// domain + sep + ts + pid_len + pid + nonce + hash
	b := make([]byte, 0, len(domain)+1+8+2+len(producerID)+NonceSize+32)
	b = append(b, domain...)
	b = append(b, 0x00)
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], uint64(ts))
	b = append(b, t[:]...)
	var pl [2]byte
	binary.BigEndian.PutUint16(pl[:], uint16(len(producerID)))
	b = append(b, pl[:]...)
	b = append(b, producerID...)
	b = append(b, nonce...)
	b = append(b, contentHash[:]...)
	return b, nil
}
