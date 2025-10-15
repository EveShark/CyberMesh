package state

import (
	"time"
)

type TxType string

const (
	TxEvent    TxType = "event"
	TxEvidence TxType = "evidence"
	TxPolicy   TxType = "policy"
)

type Transaction interface {
	Type() TxType
	Timestamp() int64
	Payload() []byte
	Envelope() *Envelope
	Canonical() ([]byte, error)
	Hash() ([32]byte, error)
	Validate(now time.Time, skew time.Duration) error
}

// Envelope contains mandatory metadata for producer authentication and replay protection
type Envelope struct {
	ProducerID  []byte
	Nonce       []byte
	ContentHash [32]byte
	Alg         string
	PubKey      []byte
	Signature   []byte
}

// CoCEntry represents a chain-of-custody item for evidence
type CoCEntry struct {
	RefHash   [32]byte
	ActorID   []byte
	Ts        int64
	Signature []byte
}

type EventTx struct {
	Ts   int64
	Data []byte
	Env  Envelope
}

func (t *EventTx) Type() TxType               { return TxEvent }
func (t *EventTx) Timestamp() int64           { return t.Ts }
func (t *EventTx) Payload() []byte            { return t.Data }
func (t *EventTx) Envelope() *Envelope        { return &t.Env }
func (t *EventTx) Canonical() ([]byte, error) { return CanonicalTxBytes(DomainEventTx, t.Ts, t.Data) }
func (t *EventTx) Hash() ([32]byte, error)    { return TxHash(DomainEventTx, t.Ts, t.Data) }
func (t *EventTx) Validate(now time.Time, skew time.Duration) error {
	if err := ValidateSize(len(t.Data), MaxTxPayloadSize); err != nil {
		return err
	}
	if err := CheckSkew(t.Ts, now, skew); err != nil {
		return err
	}
	// Envelope checks (no cryptographic verify here; done at ingestion)
	if len(t.Env.ProducerID) == 0 || len(t.Env.ProducerID) > MaxProducerIDSize {
		return ErrInvalidKey
	}
	if len(t.Env.Nonce) != NonceSize {
		return ErrInvalidKey
	}
	if ContentHash(t.Data) != t.Env.ContentHash {
		return ErrInvalidValue
	}
	if len(t.Env.Alg) == 0 || len(t.Env.Alg) > MaxAlgSize {
		return ErrInvalidValue
	}
	// PubKey/Signature presence is mandatory; size checked by ingestion based on Alg
	if len(t.Env.PubKey) == 0 || len(t.Env.Signature) == 0 {
		return ErrInvalidValue
	}
	return nil
}

type EvidenceTx struct {
	Ts   int64
	Data []byte
	Env  Envelope
	CoC  []CoCEntry
}

func (t *EvidenceTx) Type() TxType        { return TxEvidence }
func (t *EvidenceTx) Timestamp() int64    { return t.Ts }
func (t *EvidenceTx) Payload() []byte     { return t.Data }
func (t *EvidenceTx) Envelope() *Envelope { return &t.Env }
func (t *EvidenceTx) Canonical() ([]byte, error) {
	return CanonicalTxBytes(DomainEvidenceTx, t.Ts, t.Data)
}
func (t *EvidenceTx) Hash() ([32]byte, error) { return TxHash(DomainEvidenceTx, t.Ts, t.Data) }
func (t *EvidenceTx) Validate(now time.Time, skew time.Duration) error {
	if err := ValidateSize(len(t.Data), MaxTxPayloadSize); err != nil {
		return err
	}
	if err := CheckSkew(t.Ts, now, skew); err != nil {
		return err
	}
	if len(t.Env.ProducerID) == 0 || len(t.Env.ProducerID) > MaxProducerIDSize {
		return ErrInvalidKey
	}
	if len(t.Env.Nonce) != NonceSize {
		return ErrInvalidKey
	}
	if ContentHash(t.Data) != t.Env.ContentHash {
		return ErrInvalidValue
	}
	if len(t.Env.Alg) == 0 || len(t.Env.Alg) > MaxAlgSize {
		return ErrInvalidValue
	}
	if len(t.Env.PubKey) == 0 || len(t.Env.Signature) == 0 {
		return ErrInvalidValue
	}
	// Chain of custody checks (bounded)
	if len(t.CoC) == 0 || len(t.CoC) > MaxCoCEntries {
		return ErrInvalidValue
	}
	for _, c := range t.CoC {
		if len(c.ActorID) == 0 || len(c.ActorID) > MaxActorIDSize {
			return ErrInvalidValue
		}
		// signatures in CoC verified at ingestion; here ensure presence
		if len(c.Signature) == 0 {
			return ErrInvalidValue
		}
	}
	return nil
}

type PolicyTx struct {
	Ts   int64
	Data []byte
	Env  Envelope
}

func (t *PolicyTx) Type() TxType               { return TxPolicy }
func (t *PolicyTx) Timestamp() int64           { return t.Ts }
func (t *PolicyTx) Payload() []byte            { return t.Data }
func (t *PolicyTx) Envelope() *Envelope        { return &t.Env }
func (t *PolicyTx) Canonical() ([]byte, error) { return CanonicalTxBytes(DomainPolicyTx, t.Ts, t.Data) }
func (t *PolicyTx) Hash() ([32]byte, error)    { return TxHash(DomainPolicyTx, t.Ts, t.Data) }
func (t *PolicyTx) Validate(now time.Time, skew time.Duration) error {
	if err := ValidateSize(len(t.Data), MaxTxPayloadSize); err != nil {
		return err
	}
	if err := CheckSkew(t.Ts, now, skew); err != nil {
		return err
	}
	if len(t.Env.ProducerID) == 0 || len(t.Env.ProducerID) > MaxProducerIDSize {
		return ErrInvalidKey
	}
	if len(t.Env.Nonce) != NonceSize {
		return ErrInvalidKey
	}
	if ContentHash(t.Data) != t.Env.ContentHash {
		return ErrInvalidValue
	}
	if len(t.Env.Alg) == 0 || len(t.Env.Alg) > MaxAlgSize {
		return ErrInvalidValue
	}
	if len(t.Env.PubKey) == 0 || len(t.Env.Signature) == 0 {
		return ErrInvalidValue
	}
	return nil
}
