package utils

import "math"

const (
	// Millisecond epoch bounds for years 2000-2100 (used for conservative unit heuristics).
	epochMsYear2000 = int64(946684800000)
	epochMsYear2100 = int64(4102444800000)
)

// NormalizeUnixMillis normalizes unix timestamps that may be encoded in seconds/milliseconds/microseconds/nanoseconds.
// It returns:
// - normalized timestamp in milliseconds
// - corrected=true when unit conversion was applied
// - valid=false when input is non-positive or would overflow during conversion
func NormalizeUnixMillis(ts int64) (normalized int64, corrected bool, valid bool) {
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
