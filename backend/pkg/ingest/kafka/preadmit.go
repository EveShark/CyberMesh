package kafka

import (
	"errors"
	"strings"
)

// ErrPreAdmitRejected identifies deterministic pre-admit rejections.
var ErrPreAdmitRejected = errors.New("kafka: pre-admit rejected")

type preAdmitRejected struct {
	reason string
	cause  error
}

func (e preAdmitRejected) Error() string {
	if e.cause != nil {
		return e.reason + ": " + e.cause.Error()
	}
	return e.reason
}

func (e preAdmitRejected) Unwrap() error { return e.cause }

func (e preAdmitRejected) Is(target error) bool { return target == ErrPreAdmitRejected }

func NewPreAdmitRejected(reason string, cause error) error {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	if normalized == "" {
		normalized = "pre_admit_rejected"
	}
	return preAdmitRejected{reason: normalized, cause: cause}
}

func PreAdmitReason(err error) (string, bool) {
	var pErr preAdmitRejected
	if !errors.As(err, &pErr) {
		return "", false
	}
	return pErr.reason, true
}
