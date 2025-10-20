package api

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// parseUint64 parses a string to uint64 with validation
func parseUint64(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uint64: %w", err)
	}

	return val, nil
}

// parseInt parses a string to int with validation
func parseInt(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %w", err)
	}

	return val, nil
}

// parseHex parses a hex-encoded string with optional 0x prefix
func parseHex(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty hex string")
	}

	// Remove 0x prefix if present
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	// Validate hex characters
	for i, c := range s {
		if !isHexChar(c) {
			return nil, fmt.Errorf("invalid hex character at position %d: %c", i, c)
		}
	}

	// Decode
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("hex decode failed: %w", err)
	}

	return data, nil
}

// isHexChar checks if a rune is a valid hex character
func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// encodeHex encodes bytes to hex string with 0x prefix
func encodeHex(data []byte) string {
	if len(data) == 0 {
		return "0x"
	}
	return "0x" + hex.EncodeToString(data)
}

// validateBlockHeight validates a block height against current height
func validateBlockHeight(height, currentHeight uint64) error {
	if height > currentHeight {
		return NewInvalidHeightError(height, currentHeight)
	}
	return nil
}

// validateStateKey validates a state key
func validateStateKey(key string) error {
	if key == "" {
		return NewInvalidKeyError(key, "key cannot be empty")
	}

	// Check if valid hex
	_, err := parseHex(key)
	if err != nil {
		return NewInvalidKeyError(key, "key must be valid hex")
	}

	// Reasonable max size (prevent DoS)
	const maxKeySize = 256 // bytes (512 hex chars)
	if len(key) > maxKeySize*2 {
		return NewInvalidKeyError(key, fmt.Sprintf("key too large (max %d bytes)", maxKeySize))
	}

	return nil
}

// validatePaginationLimit validates and constrains pagination limit
func validatePaginationLimit(limit int) (int, error) {
	const (
		defaultLimit = 10
		maxLimit     = 100
	)

	if limit <= 0 {
		return defaultLimit, nil
	}

	if limit > maxLimit {
		return maxLimit, nil
	}

	return limit, nil
}

// sanitizeString sanitizes a string input to prevent injection attacks
func sanitizeString(s string) string {
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")

	// Remove control characters (except tab, newline, carriage return)
	var result strings.Builder
	for _, r := range s {
		if r >= 32 || r == '\t' || r == '\n' || r == '\r' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// validateValidatorID validates a validator ID
func validateValidatorID(id string) error {
	if id == "" {
		return NewInvalidParamsError("id", "validator ID cannot be empty")
	}

	// Sanitize
	id = sanitizeString(id)

	// Reasonable max length
	const maxIDLength = 128
	if len(id) > maxIDLength {
		return NewInvalidParamsError("id", fmt.Sprintf("validator ID too long (max %d)", maxIDLength))
	}

	return nil
}

// parseBool parses a boolean query parameter
func parseBool(s string) bool {
	if s == "" {
		return false
	}

	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

// validateStatus validates a status filter
func validateStatus(status string) error {
	if status == "" {
		return nil // empty is valid (no filter)
	}

	status = strings.ToLower(strings.TrimSpace(status))

	validStatuses := map[string]bool{
		"active":   true,
		"inactive": true,
		"all":      true,
	}

	if !validStatuses[status] {
		return NewInvalidParamsError("status", "must be one of: active, inactive, all")
	}

	return nil
}

// extractPathParam extracts a path parameter from URL
// Example: /blocks/42 -> extractPathParam(path, "/blocks/", 1) -> "42"
func extractPathParam(path string, prefix string, index int) (string, error) {
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("path does not start with %s", prefix)
	}

	// Remove prefix
	rest := strings.TrimPrefix(path, prefix)

	// Split by /
	parts := strings.Split(rest, "/")

	if index >= len(parts) {
		return "", fmt.Errorf("path parameter index %d out of range", index)
	}

	param := parts[index]
	if param == "" {
		return "", fmt.Errorf("empty path parameter at index %d", index)
	}

	return param, nil
}

// getQueryParam extracts a query parameter from URL query string
func getQueryParam(queryString, param string) string {
	// Simple query parameter extraction
	// In production, use url.Parse() but this avoids external deps in example

	params := strings.Split(queryString, "&")
	for _, p := range params {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 && kv[0] == param {
			return kv[1]
		}
	}

	return ""
}

// Helper for extracting client certificate common name (CN)
func extractCN(certSubject string) string {
	// certSubject format: "CN=admin,OU=validators,O=cybermesh"
	parts := strings.Split(certSubject, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "CN=") {
			return strings.TrimPrefix(part, "CN=")
		}
	}
	return ""
}

// Helper for validating request ID format
func isValidRequestID(id string) bool {
	if id == "" {
		return false
	}

	// Allow alphanumeric, hyphens, underscores
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}

	const maxIDLength = 128
	return len(id) <= maxIDLength
}

// clamp clamps a value between min and max
func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
