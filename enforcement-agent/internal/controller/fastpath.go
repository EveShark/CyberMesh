package controller

import "github.com/CyberMesh/enforcement-agent/internal/policy"

// FastPathEligibility expresses whether a policy qualifies for pre-consensus enforcement.
type FastPathEligibility struct {
	Eligible bool
	Reason   string
}

const (
	fastPathSkipDisabled          = "disabled"
	fastPathSkipGuardrail         = "guardrail_disabled"
	fastPathSkipTTLMissing        = "ttl_missing"
	fastPathSkipSignalsMissing    = "signals_missing"
	fastPathSkipConfidenceMissing = "confidence_missing"
	fastPathSkipConfidenceLow     = "confidence_insufficient"
)

// EvaluateFastPath determines whether the given spec is eligible for fast-path.
func EvaluateFastPath(spec policy.PolicySpec, cfg FastPathConfig) FastPathEligibility {
	guard := spec.Guardrails
	if !cfg.Enabled || !guard.FastPathEnabled {
		return FastPathEligibility{Reason: fastPathSkipDisabled}
	}
	if guard.FastPathTTLSeconds == nil || *guard.FastPathTTLSeconds <= 0 {
		return FastPathEligibility{Reason: fastPathSkipTTLMissing}
	}
	requiredSignals := cfg.SignalsRequired
	if guard.FastPathSignalsRequired != nil {
		requiredSignals = *guard.FastPathSignalsRequired
	}
	if requiredSignals <= 0 {
		return FastPathEligibility{Reason: fastPathSkipSignalsMissing}
	}
	minConfidence := cfg.MinConfidence
	if guard.FastPathConfidenceMin != nil {
		minConfidence = *guard.FastPathConfidenceMin
	}
	if spec.Criteria.MinConfidence == nil || *spec.Criteria.MinConfidence < minConfidence {
		return FastPathEligibility{Reason: fastPathSkipConfidenceLow}
	}
	return FastPathEligibility{Eligible: true}
}
