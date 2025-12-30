package state

import (
	"encoding/binary"
)

// Key namespaces (byte prefixes)
var (
	prefEvent     = []byte("evt:")
	prefEvidence  = []byte("evi:")
	prefCoC       = []byte("coc:")
	prefProducerL = []byte("prod:last:")
	prefProducerC = []byte("prod:cnt:")
	prefNonce     = []byte("nonce:")
	prefPolicy    = []byte("pol:")
	keyPolicyHead = []byte("pol:latest")
)

func u64(n uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	return b[:]
}

func i64(n int64) []byte { return u64(uint64(n)) }

func keyEvent(h [32]byte) []byte    { return append(append([]byte(nil), prefEvent...), h[:]...) }
func keyEvidence(h [32]byte) []byte { return append(append([]byte(nil), prefEvidence...), h[:]...) }
func keyCoC(h [32]byte, idx uint16) []byte {
	k := append(append([]byte(nil), prefCoC...), h[:]...)
	var id [2]byte
	binary.BigEndian.PutUint16(id[:], idx)
	return append(k, id[:]...)
}
func keyProducerLast(pid []byte) []byte { return append(append([]byte(nil), prefProducerL...), pid...) }
func keyProducerCnt(pid []byte) []byte  { return append(append([]byte(nil), prefProducerC...), pid...) }
func keyNonce(pid, nonce []byte) []byte {
	k := append(append([]byte(nil), prefNonce...), pid...)
	return append(k, nonce...)
}

// NonceKey returns the canonical state key for the given producer nonce.
// It must match the key format used by reducers when recording nonce->contentHash.
func NonceKey(producerID, nonce []byte) []byte { return keyNonce(producerID, nonce) }
func keyPolicy(h [32]byte) []byte              { return append(append([]byte(nil), prefPolicy...), h[:]...) }

// ApplyEvent stores the event payload, updates producer last-ts and count, and records nonce->hash mapping.
func ApplyEvent(tx *EventTx, txn *Txn) error {
	// store event payload under content hash
	if err := txn.Put(keyEvent(tx.Env.ContentHash), tx.Data); err != nil {
		return err
	}
	// producer metadata
	if err := txn.Put(keyProducerLast(tx.Env.ProducerID), i64(tx.Ts)); err != nil {
		return err
	}
	// count increment (read-modify-write; bounded to 8 bytes)
	cntB, _ := txn.Get(keyProducerCnt(tx.Env.ProducerID))
	var cnt uint64
	if len(cntB) == 8 {
		cnt = binary.BigEndian.Uint64(cntB)
	}
	cnt++
	if err := txn.Put(keyProducerCnt(tx.Env.ProducerID), u64(cnt)); err != nil {
		return err
	}
	// nonce tracking (for forensics; admission ensures uniqueness)
	if err := txn.Put(keyNonce(tx.Env.ProducerID, tx.Env.Nonce), tx.Env.ContentHash[:]); err != nil {
		return err
	}
	return nil
}

// ApplyEvidence stores evidence payload, chain-of-custody entries, updates producer metadata, and records nonce.
func ApplyEvidence(tx *EvidenceTx, txn *Txn) error {
	if err := txn.Put(keyEvidence(tx.Env.ContentHash), tx.Data); err != nil {
		return err
	}
	for i := range tx.CoC {
		if err := txn.Put(keyCoC(tx.Env.ContentHash, uint16(i)), tx.CoC[i].RefHash[:]); err != nil {
			return err
		}
	}
	if err := txn.Put(keyProducerLast(tx.Env.ProducerID), i64(tx.Ts)); err != nil {
		return err
	}
	cntB, _ := txn.Get(keyProducerCnt(tx.Env.ProducerID))
	var cnt uint64
	if len(cntB) == 8 {
		cnt = binary.BigEndian.Uint64(cntB)
	}
	cnt++
	if err := txn.Put(keyProducerCnt(tx.Env.ProducerID), u64(cnt)); err != nil {
		return err
	}
	if err := txn.Put(keyNonce(tx.Env.ProducerID, tx.Env.Nonce), tx.Env.ContentHash[:]); err != nil {
		return err
	}
	return nil
}

// ApplyPolicy stores the policy payload and advances latest pointer, updates producer metadata, and records nonce.
func ApplyPolicy(tx *PolicyTx, txn *Txn) error {
	if err := txn.Put(keyPolicy(tx.Env.ContentHash), tx.Data); err != nil {
		return err
	}
	if err := txn.Put(keyPolicyHead, tx.Env.ContentHash[:]); err != nil {
		return err
	}
	if err := txn.Put(keyProducerLast(tx.Env.ProducerID), i64(tx.Ts)); err != nil {
		return err
	}
	cntB, _ := txn.Get(keyProducerCnt(tx.Env.ProducerID))
	var cnt uint64
	if len(cntB) == 8 {
		cnt = binary.BigEndian.Uint64(cntB)
	}
	cnt++
	if err := txn.Put(keyProducerCnt(tx.Env.ProducerID), u64(cnt)); err != nil {
		return err
	}
	if err := txn.Put(keyNonce(tx.Env.ProducerID, tx.Env.Nonce), tx.Env.ContentHash[:]); err != nil {
		return err
	}
	return nil
}
