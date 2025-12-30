package api

import (
	"errors"
	"fmt"
)

// InvalidBlockError indicates that a block is deterministically invalid at the application/state layer.
// It must abort commit (safety) but must not halt the entire consensus engine (liveness).
type InvalidBlockError struct {
	Cause error
}

func (e *InvalidBlockError) Error() string {
	if e == nil {
		return "invalid block"
	}
	if e.Cause == nil {
		return "invalid block"
	}
	return fmt.Sprintf("invalid block: %v", e.Cause)
}

func (e *InvalidBlockError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsInvalidBlock reports whether err (possibly wrapped) represents an InvalidBlockError.
func IsInvalidBlock(err error) bool {
	var ibe *InvalidBlockError
	return errors.As(err, &ibe)
}
