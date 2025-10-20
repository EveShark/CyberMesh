package api

import (
	"backend/pkg/utils"
)

// API-specific error codes (extending utils.ErrorCode)
const (
	// Request errors
	ErrCodeInvalidRequest utils.ErrorCode = "INVALID_REQUEST"
	ErrCodeInvalidHeight  utils.ErrorCode = "INVALID_HEIGHT"
	ErrCodeInvalidKey     utils.ErrorCode = "INVALID_KEY"
	ErrCodeInvalidParams  utils.ErrorCode = "INVALID_PARAMS"
	ErrCodeMalformedJSON  utils.ErrorCode = "MALFORMED_JSON"

	// Resource errors
	ErrCodeBlockNotFound     utils.ErrorCode = "BLOCK_NOT_FOUND"
	ErrCodeStateNotFound     utils.ErrorCode = "STATE_NOT_FOUND"
	ErrCodeValidatorNotFound utils.ErrorCode = "VALIDATOR_NOT_FOUND"

	// Security errors (reuse from utils where possible)
	ErrCodeUnauthorized = utils.CodeUnauthorized
	ErrCodeForbidden    = utils.CodeForbidden
	ErrCodeRateLimited  = utils.CodeRateLimited

	// Server errors
	ErrCodeInternal    = utils.CodeInternal
	ErrCodeUnavailable = utils.CodeUnavailable
	ErrCodeTimeout     = utils.CodeTimeout
)

// NewInvalidRequestError creates an invalid request error
func NewInvalidRequestError(message string) *utils.Error {
	return utils.NewError(ErrCodeInvalidRequest, message).
		WithHTTPStatus(400)
}

// NewInvalidHeightError creates an invalid height error
func NewInvalidHeightError(height uint64, currentHeight uint64) *utils.Error {
	return utils.NewError(ErrCodeInvalidHeight, "invalid block height").
		WithDetail("requested_height", height).
		WithDetail("current_height", currentHeight).
		WithHTTPStatus(400)
}

// NewInvalidKeyError creates an invalid key error
func NewInvalidKeyError(key string, reason string) *utils.Error {
	return utils.NewError(ErrCodeInvalidKey, "invalid state key").
		WithDetail("key", key).
		WithDetail("reason", reason).
		WithHTTPStatus(400)
}

// NewInvalidParamsError creates an invalid parameters error
func NewInvalidParamsError(param string, reason string) *utils.Error {
	return utils.NewError(ErrCodeInvalidParams, "invalid query parameter").
		WithDetail("parameter", param).
		WithDetail("reason", reason).
		WithHTTPStatus(400)
}

// NewMalformedJSONError creates a malformed JSON error
func NewMalformedJSONError(err error) *utils.Error {
	return utils.WrapError(err, ErrCodeMalformedJSON, "malformed JSON in request body").
		WithHTTPStatus(400)
}

// NewBlockNotFoundError creates a block not found error
func NewBlockNotFoundError(height uint64) *utils.Error {
	return utils.NewError(ErrCodeBlockNotFound, "block not found").
		WithDetail("height", height).
		WithHTTPStatus(404)
}

// NewStateNotFoundError creates a state not found error
func NewStateNotFoundError(key string) *utils.Error {
	return utils.NewError(ErrCodeStateNotFound, "state key not found").
		WithDetail("key", key).
		WithHTTPStatus(404)
}

// NewValidatorNotFoundError creates a validator not found error
func NewValidatorNotFoundError(id string) *utils.Error {
	return utils.NewError(ErrCodeValidatorNotFound, "validator not found").
		WithDetail("id", id).
		WithHTTPStatus(404)
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError(reason string) *utils.Error {
	return utils.NewError(ErrCodeUnauthorized, "authentication failed").
		WithDetail("reason", reason).
		WithHTTPStatus(401)
}

// NewForbiddenError creates a forbidden error
func NewForbiddenError(reason string) *utils.Error {
	return utils.NewError(ErrCodeForbidden, "access denied").
		WithDetail("reason", reason).
		WithHTTPStatus(403)
}

// NewRateLimitError creates a rate limit exceeded error
func NewRateLimitError(limit int, resetTime int64) *utils.Error {
	return utils.NewError(ErrCodeRateLimited, "rate limit exceeded").
		WithDetail("limit", limit).
		WithDetail("reset_time", resetTime).
		WithHTTPStatus(429)
}

// NewInternalError creates an internal server error
func NewInternalError(message string) *utils.Error {
	return utils.NewError(ErrCodeInternal, message).
		WithHTTPStatus(500)
}

// NewUnavailableError creates a service unavailable error
func NewUnavailableError(component string) *utils.Error {
	return utils.NewError(ErrCodeUnavailable, "service temporarily unavailable").
		WithDetail("component", component).
		WithHTTPStatus(503)
}

// NewTimeoutError creates a timeout error
func NewTimeoutError(operation string) *utils.Error {
	return utils.NewError(ErrCodeTimeout, "operation timed out").
		WithDetail("operation", operation).
		WithHTTPStatus(504)
}
