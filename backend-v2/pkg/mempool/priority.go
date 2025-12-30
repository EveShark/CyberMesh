package mempool

import "backend/pkg/state"

// ComputeMeta derives admission meta from tx payload context.
// In production, this should parse canonical payload fields for severity/confidence.
// Here we accept provided meta and ensure bounds.
func ComputeMeta(tx state.Transaction, provided AdmissionMeta) AdmissionMeta {
	m := provided
	// clamp values to safe ranges
	if m.Confidence < 0 {
		m.Confidence = 0
	}
	if m.Confidence > 1 {
		m.Confidence = 1
	}
	return m
}
