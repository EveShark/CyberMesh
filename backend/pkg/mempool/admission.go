package mempool

import (
	"crypto/ed25519"
	"errors"
	"strings"
	"time"

	"backend/pkg/state"
)

var (
	ErrSigInvalid = errors.New("invalid signature")
)

// VerifyEnvelope performs cryptographic verification of a transaction envelope.
// Signature domain: domain||0x00||ts||producer_id_len||producer_id||nonce||content_hash
func VerifyEnvelope(tx state.Transaction) error {
	env := tx.Envelope()
	if env == nil {
		return ErrInvalidTx
	}
	// currently support Ed25519 only
	alg := strings.ToUpper(env.Alg)
	if alg != "ED25519" {
		return ErrInvalidTx
	}
	// verify content hash matches payload
	if state.ContentHash(tx.Payload()) != env.ContentHash {
		return ErrInvalidTx
	}
	// build sign bytes using tx domain and ts
	domain := domainFor(tx.Type())
	b, err := state.BuildSignBytes(domain, tx.Timestamp(), env.ProducerID, env.Nonce, env.ContentHash)
	if err != nil {
		return err
	}
	if len(env.PubKey) != state.PubKeyEd25519Size || len(env.Signature) != state.SigEd25519Size {
		return ErrSigInvalid
	}
	if !ed25519.Verify(ed25519.PublicKey(env.PubKey), b[:], env.Signature) {
		return ErrSigInvalid
	}
	return nil
}

func domainFor(t state.TxType) string {
	switch t {
	case state.TxEvent:
		return state.DomainEventTx
	case state.TxEvidence:
		return state.DomainEvidenceTx
	case state.TxPolicy:
		return state.DomainPolicyTx
	default:
		return ""
	}
}

// Admit performs full admission: crypto verify, tx validation, skew/rate/nonce checks (via Mempool.Add).
func (m *Mempool) Admit(tx state.Transaction, meta AdmissionMeta, now time.Time) error {
	if err := VerifyEnvelope(tx); err != nil {
		return err
	}
	return m.Add(tx, meta, now)
}
