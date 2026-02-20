package dlq

import (
	"crypto/sha256"
	"fmt"
	"time"
)

func NewEnvelope(schema, source, code, message string, payload []byte) Envelope {
	hash := sha256.Sum256(payload)
	return Envelope{
		Schema:          schema,
		Timestamp:       time.Now().Unix(),
		SourceComponent: source,
		ErrorCode:       code,
		ErrorMessage:    message,
		PayloadHash:     fmt.Sprintf("%x", hash[:]),
	}
}
