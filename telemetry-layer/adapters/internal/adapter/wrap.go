package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

func NewEnvelope(schema, source, code, message string, payload []byte) Envelope {
	return Envelope{
		Schema:          schema,
		Timestamp:       time.Now().Unix(),
		SourceComponent: source,
		ErrorCode:       code,
		ErrorMessage:    message,
		PayloadHash:     hashPayload(payload),
	}
}

func hashPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
