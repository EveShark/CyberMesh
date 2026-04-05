package utils

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

const (
	// Millisecond epoch bounds for years 2000-2100 (used for conservative unit heuristics).
	epochMsYear2000 = int64(946684800000)
	epochMsYear2100 = int64(4102444800000)
)

type TimestampNormalizeMode int

const (
	// TimestampNormalizeStrictEpoch accepts only epoch-based seconds/ms/us/ns encodings.
	TimestampNormalizeStrictEpoch TimestampNormalizeMode = iota
	// TimestampNormalizeTraceCompatible also accepts legacy small relative milliseconds.
	TimestampNormalizeTraceCompatible
)

// NormalizeUnixMillis normalizes unix timestamps that may be encoded in seconds/milliseconds/microseconds/nanoseconds.
// It returns:
// - normalized timestamp in milliseconds
// - corrected=true when unit conversion was applied
// - valid=false when input is non-positive or would overflow during conversion
func NormalizeUnixMillis(ts int64) (normalized int64, corrected bool, valid bool) {
	return NormalizeTimestampMs(ts, TimestampNormalizeStrictEpoch)
}

func normalizeStrictEpochMillis(ts int64) (normalized int64, corrected bool, valid bool) {
	if ts <= 0 {
		return 0, false, false
	}

	abs := ts
	if abs < 0 {
		abs = -abs
	}
	// Avoid MinInt64 overflow on abs.
	if abs < 0 {
		return 0, false, false
	}

	// Seconds (10 digits for contemporary epochs): convert to milliseconds.
	if abs < epochMsYear2000 {
		if abs > math.MaxInt64/1000 {
			return 0, false, false
		}
		n := ts * 1000
		if n < epochMsYear2000 || n > epochMsYear2100 {
			return 0, true, false
		}
		return n, true, true
	}

	// Milliseconds: already normalized.
	if abs <= epochMsYear2100 {
		return ts, false, true
	}

	// Microseconds: convert to milliseconds.
	if abs <= epochMsYear2100*10000 {
		n := ts / 1000
		if n < epochMsYear2000 || n > epochMsYear2100 {
			return 0, true, false
		}
		return n, true, true
	}

	// Nanoseconds and above: convert to milliseconds.
	n := ts / 1_000_000
	if n < epochMsYear2000 || n > epochMsYear2100 {
		return 0, true, false
	}
	return n, true, true
}

// NormalizeTimestampMs normalizes timestamp values to epoch milliseconds using
// the selected mode.
func NormalizeTimestampMs(ts int64, mode TimestampNormalizeMode) (normalized int64, corrected bool, valid bool) {
	switch mode {
	case TimestampNormalizeStrictEpoch:
		return normalizeStrictEpochMillis(ts)
	case TimestampNormalizeTraceCompatible:
		if ts > 0 && ts < 946684800 {
			return ts, false, true
		}
		return normalizeStrictEpochMillis(ts)
	default:
		return 0, false, false
	}
}

// NormalizeTimestampMsAny parses timestamp values from common payload types and
// normalizes them using the selected mode.
func NormalizeTimestampMsAny(v interface{}, mode TimestampNormalizeMode) (normalized int64, corrected bool, valid bool) {
	switch n := v.(type) {
	case int64:
		return NormalizeTimestampMs(n, mode)
	case float64:
		return NormalizeTimestampMs(int64(n), mode)
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			return NormalizeTimestampMs(parsed, mode)
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return NormalizeTimestampMs(parsed, mode)
		}
	}
	return 0, false, false
}

// NormalizeEpochMillis is an alias for NormalizeUnixMillis for call-sites that
// need explicit epoch-millisecond naming.
func NormalizeEpochMillis(ts int64) (normalized int64, corrected bool, valid bool) {
	return NormalizeTimestampMs(ts, TimestampNormalizeStrictEpoch)
}

// DurationMillis computes end-start for millisecond epoch values.
// It returns:
// - duration in ms when ok=true
// - ok=false when either endpoint is non-positive or end < start
// - negative=true only when both endpoints are valid but end < start
func DurationMillis(startMs, endMs int64) (duration int64, ok bool, negative bool) {
	if startMs <= 0 || endMs <= 0 {
		return 0, false, false
	}
	if endMs < startMs {
		return 0, false, true
	}
	return endMs - startMs, true, false
}
