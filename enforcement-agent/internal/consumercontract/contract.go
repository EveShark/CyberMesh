package consumercontract

import (
	"context"
	"errors"
)

// Phase identifies the canonical consumer state-machine stage.
type Phase string

const (
	PhaseDecodeVerify   Phase = "decode_verify"
	PhaseClassifyError  Phase = "classify_error"
	PhaseRetryPolicy    Phase = "retry_policy"
	PhaseTerminalAction Phase = "terminal_action"
	PhaseMarkDecision   Phase = "mark_decision"
)

// ErrorClass identifies canonical failure classes across consumers.
type ErrorClass string

const (
	ErrorClassPermanent ErrorClass = "permanent"
	ErrorClassTransient ErrorClass = "transient"
	ErrorClassTimeout   ErrorClass = "timeout"
	ErrorClassCanceled  ErrorClass = "canceled"
)

// ClassifyContextError maps context termination errors to canonical classes.
func ClassifyContextError(err error) (ErrorClass, bool) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorClassTimeout, true
	case errors.Is(err, context.Canceled):
		return ErrorClassCanceled, true
	default:
		return "", false
	}
}
