package utils

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Error categories
const (
	CategoryUnknown    = "unknown"
	CategoryValidation = "validation"
	CategoryAuth       = "authentication"
	CategoryPermission = "permission"
	CategoryNetwork    = "network"
	CategoryTimeout    = "timeout"
	CategoryResource   = "resource"
	CategoryCrypto     = "cryptography"
	CategoryConsensus  = "consensus"
	CategoryRateLimit  = "rate_limit"
	CategoryCircuit    = "circuit_breaker"
	CategoryInternal   = "internal"
)

// Base error codes
const (
	CodeUnknown            = "UNKNOWN"
	CodeInvalidInput       = "INVALID_INPUT"
	CodeInvalidSignature   = "INVALID_SIGNATURE"
	CodeReplayDetected     = "REPLAY_DETECTED"
	CodeNotLeader          = "NOT_LEADER"
	CodeQuorumNotMet       = "QUORUM_NOT_MET"
	CodeConfigInvalid      = "CONFIG_INVALID"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbidden          = "FORBIDDEN"
	CodeIPNotAllowed       = "IP_NOT_ALLOWED"
	CodeKeysMissing        = "KEYS_MISSING"
	CodeTimeout            = "TIMEOUT"
	CodeDeadlineExceeded   = "DEADLINE_EXCEEDED"
	CodeUnavailable        = "UNAVAILABLE"
	CodeResourceExhausted  = "RESOURCE_EXHAUSTED"
	CodeRateLimited        = "RATE_LIMITED"
	CodeCircuitOpen        = "CIRCUIT_OPEN"
	CodeNodeDown           = "NODE_DOWN"
	CodeNodeUnreachable    = "NODE_UNREACHABLE"
	CodeDataCorrupted      = "DATA_CORRUPTED"
	CodeConflict           = "CONFLICT"
	CodeNotFound           = "NOT_FOUND"
	CodeAlreadyExists      = "ALREADY_EXISTS"
	CodePreconditionFailed = "PRECONDITION_FAILED"
	CodeInternal           = "INTERNAL_ERROR"
)

// ErrorCode represents a machine-readable error identifier
type ErrorCode string

// ErrorCategory groups related errors
type ErrorCategory string

// Error provides structured error information
type Error struct {
	Code       ErrorCode
	Category   ErrorCategory
	Message    string
	Details    map[string]interface{}
	Underlying error
	Retryable  bool
	Temporary  bool
	HTTPStatus int
	Timestamp  time.Time
	Stack      []StackFrame
}

// StackFrame represents a single stack frame
type StackFrame struct {
	File     string
	Line     int
	Function string
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Underlying)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap implements error unwrapping
func (e *Error) Unwrap() error {
	return e.Underlying
}

// Is implements error comparison
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// WithDetail adds a detail field to the error
func (e *Error) WithDetail(key string, value interface{}) *Error {
	if e.Details == nil {
		e.Details = make(map[string]interface{})
	}
	e.Details[key] = value
	return e
}

// WithHTTPStatus sets the HTTP status code
func (e *Error) WithHTTPStatus(status int) *Error {
	e.HTTPStatus = status
	return e
}

// IsRetryable returns whether the error should be retried
func (e *Error) IsRetryable() bool {
	return e.Retryable
}

// IsTemporary returns whether the error is temporary
func (e *Error) IsTemporary() bool {
	return e.Temporary
}

// GetHTTPStatus returns the appropriate HTTP status code
func (e *Error) GetHTTPStatus() int {
	if e.HTTPStatus > 0 {
		return e.HTTPStatus
	}
	return errorCodeToHTTPStatus(e.Code)
}

// NewError creates a new structured error
func NewError(code ErrorCode, message string) *Error {
	return &Error{
		Code:      code,
		Category:  getCategory(code),
		Message:   message,
		Timestamp: time.Now(),
		Stack:     captureStack(2),
	}
}

// NewErrorf creates a new structured error with formatting
func NewErrorf(code ErrorCode, format string, args ...interface{}) *Error {
	return NewError(code, fmt.Sprintf(format, args...))
}

// WrapError wraps an existing error with structured information
func WrapError(err error, code ErrorCode, message string) *Error {
	if err == nil {
		return nil
	}

	// If already a structured error, preserve its properties
	if e, ok := err.(*Error); ok {
		return &Error{
			Code:       code,
			Category:   getCategory(code),
			Message:    message,
			Underlying: e,
			Retryable:  e.Retryable,
			Temporary:  e.Temporary,
			Timestamp:  time.Now(),
			Stack:      captureStack(2),
		}
	}

	return &Error{
		Code:       code,
		Category:   getCategory(code),
		Message:    message,
		Underlying: err,
		Timestamp:  time.Now(),
		Stack:      captureStack(2),
	}
}

// WrapErrorf wraps an existing error with formatted message
func WrapErrorf(err error, code ErrorCode, format string, args ...interface{}) *Error {
	return WrapError(err, code, fmt.Sprintf(format, args...))
}

// Predefined error constructors

func NewValidationError(message string) *Error {
	return NewError(CodeInvalidInput, message).
		WithHTTPStatus(http.StatusBadRequest)
}

func NewAuthError(message string) *Error {
	return NewError(CodeUnauthorized, message).
		WithHTTPStatus(http.StatusUnauthorized)
}

func NewPermissionError(message string) *Error {
	return NewError(CodeForbidden, message).
		WithHTTPStatus(http.StatusForbidden)
}

func NewNotFoundError(message string) *Error {
	return NewError(CodeNotFound, message).
		WithHTTPStatus(http.StatusNotFound)
}

func NewConflictError(message string) *Error {
	return NewError(CodeConflict, message).
		WithHTTPStatus(http.StatusConflict)
}

func NewTimeoutError(message string) *Error {
	err := NewError(CodeTimeout, message).
		WithHTTPStatus(http.StatusGatewayTimeout)
	err.Retryable = true
	err.Temporary = true
	return err
}

func NewRateLimitError(message string) *Error {
	err := NewError(CodeRateLimited, message).
		WithHTTPStatus(http.StatusTooManyRequests)
	err.Retryable = true
	err.Temporary = true
	return err
}

func NewCircuitOpenError(message string) *Error {
	err := NewError(CodeCircuitOpen, message).
		WithHTTPStatus(http.StatusServiceUnavailable)
	err.Retryable = true
	err.Temporary = true
	return err
}

func NewUnavailableError(message string) *Error {
	err := NewError(CodeUnavailable, message).
		WithHTTPStatus(http.StatusServiceUnavailable)
	err.Retryable = true
	err.Temporary = true
	return err
}

func NewInternalError(message string) *Error {
	return NewError(CodeInternal, message).
		WithHTTPStatus(http.StatusInternalServerError)
}

// Error checking helpers

// IsRetryable returns whether an error should be retried
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var e *Error
	if errors.As(err, &e) {
		return e.IsRetryable()
	}

	// Check for temporary errors
	if te, ok := err.(interface{ Temporary() bool }); ok {
		return te.Temporary()
	}

	return false
}

// IsTemporary returns whether an error is temporary
func IsTemporary(err error) bool {
	if err == nil {
		return false
	}

	var e *Error
	if errors.As(err, &e) {
		return e.IsTemporary()
	}

	if te, ok := err.(interface{ Temporary() bool }); ok {
		return te.Temporary()
	}

	return false
}

// GetErrorCode extracts the error code from an error
func GetErrorCode(err error) ErrorCode {
	if err == nil {
		return ""
	}

	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}

	return CodeUnknown
}

// GetErrorCategory extracts the error category from an error
func GetErrorCategory(err error) ErrorCategory {
	if err == nil {
		return ""
	}

	var e *Error
	if errors.As(err, &e) {
		return e.Category
	}

	return CategoryUnknown
}

// GetHTTPStatus extracts the HTTP status from an error
func GetHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}

	var e *Error
	if errors.As(err, &e) {
		return e.GetHTTPStatus()
	}

	return http.StatusInternalServerError
}

// Private helper functions

func getCategory(code ErrorCode) ErrorCategory {
	switch code {
	case CodeInvalidInput, CodeConfigInvalid, CodePreconditionFailed:
		return CategoryValidation
	case CodeUnauthorized, CodeKeysMissing:
		return CategoryAuth
	case CodeForbidden, CodeIPNotAllowed:
		return CategoryPermission
	case CodeTimeout, CodeDeadlineExceeded:
		return CategoryTimeout
	case CodeResourceExhausted:
		return CategoryResource
	case CodeInvalidSignature, CodeReplayDetected:
		return CategoryCrypto
	case CodeNotLeader, CodeQuorumNotMet:
		return CategoryConsensus
	case CodeRateLimited:
		return CategoryRateLimit
	case CodeCircuitOpen:
		return CategoryCircuit
	case CodeNodeDown, CodeNodeUnreachable, CodeUnavailable:
		return CategoryNetwork
	case CodeInternal, CodeDataCorrupted:
		return CategoryInternal
	default:
		return CategoryUnknown
	}
}

func errorCodeToHTTPStatus(code ErrorCode) int {
	switch code {
	case CodeInvalidInput, CodeConfigInvalid:
		return http.StatusBadRequest
	case CodeUnauthorized, CodeKeysMissing:
		return http.StatusUnauthorized
	case CodeForbidden, CodeIPNotAllowed:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeAlreadyExists:
		return http.StatusConflict
	case CodePreconditionFailed:
		return http.StatusPreconditionFailed
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeTimeout, CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case CodeUnavailable, CodeCircuitOpen, CodeNodeDown, CodeNodeUnreachable:
		return http.StatusServiceUnavailable
	case CodeResourceExhausted:
		return http.StatusInsufficientStorage
	default:
		return http.StatusInternalServerError
	}
}

func captureStack(skip int) []StackFrame {
	const maxDepth = 32
	pcs := make([]uintptr, maxDepth)
	n := runtime.Callers(skip+1, pcs)

	frames := make([]StackFrame, 0, n)
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}

		file, line := fn.FileLine(pc)

		// Skip runtime frames
		if strings.HasPrefix(file, "runtime/") {
			continue
		}

		frames = append(frames, StackFrame{
			File:     file,
			Line:     line,
			Function: fn.Name(),
		})
	}

	return frames
}

// Legacy compatibility - sentinel errors
var (
	ErrTimeout          = NewTimeoutError("operation timed out")
	ErrInvalidSignature = NewError(CodeInvalidSignature, "invalid signature")
	ErrReplay           = NewError(CodeReplayDetected, "replay detected")
	ErrNotLeader        = NewError(CodeNotLeader, "not leader")
	ErrQuorumNotMet     = NewError(CodeQuorumNotMet, "quorum not met")
	ErrConfigInvalid    = NewValidationError("invalid configuration")
	ErrUnauthorized     = NewAuthError("unauthorized")
	ErrIPNotAllowed     = NewPermissionError("ip not allowed")
	ErrKeysMissing      = NewAuthError("keys missing")
)

// Wrap annotates err with msg (legacy compatibility)
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf annotates err with formatted message (legacy compatibility)
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}
