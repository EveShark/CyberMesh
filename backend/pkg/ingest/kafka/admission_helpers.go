package kafka

import "strings"

func normalizePriorityClass(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "p0":
		return "p0"
	case "p1":
		return "p1"
	default:
		return "p2"
	}
}

func sanitizeReasonKey(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	if r == "" {
		return "unknown"
	}
	return r
}
