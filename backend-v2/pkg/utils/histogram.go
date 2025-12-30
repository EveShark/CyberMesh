package utils

import (
	"math"
	"sync"
)

// HistogramBucket represents a single cumulative bucket with an inclusive upper bound.
type HistogramBucket struct {
	UpperBound float64
	Count      uint64
}

// LatencyHistogram tracks observed latencies (in milliseconds) using fixed buckets.
type LatencyHistogram struct {
	bounds []float64
	counts []uint64
	sum    float64
	total  uint64
	mu     sync.Mutex
}

// NewLatencyHistogram constructs a histogram with the provided bucket boundaries.
// The last boundary should represent +Inf; if not supplied it will be appended automatically.
func NewLatencyHistogram(bounds []float64) *LatencyHistogram {
	if len(bounds) == 0 {
		bounds = []float64{1, 5, 20, 100, 500, 1000}
	}
	sanitised := make([]float64, len(bounds))
	copy(sanitised, bounds)
	if sanitised[len(sanitised)-1] != math.Inf(1) {
		sanitised = append(sanitised, math.Inf(1))
	}
	return &LatencyHistogram{
		bounds: sanitised,
		counts: make([]uint64, len(sanitised)),
	}
}

// Observe records a latency measurement (in milliseconds).
func (h *LatencyHistogram) Observe(value float64) {
	if value < 0 {
		value = 0
	}
	h.mu.Lock()
	h.total++
	h.sum += value
	for i, bound := range h.bounds {
		if value <= bound {
			h.counts[i]++
			break
		}
	}
	h.mu.Unlock()
}

// Snapshot returns a copy of the bucket counts, total observations and cumulative sum.
func (h *LatencyHistogram) Snapshot() ([]HistogramBucket, uint64, float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HistogramBucket, len(h.bounds))
	for i := range h.bounds {
		out[i] = HistogramBucket{UpperBound: h.bounds[i], Count: h.counts[i]}
	}
	return out, h.total, h.sum
}

// Quantile estimates the latency at the given quantile (0 <= q <= 1).
// If no samples have been recorded the result is 0.
func (h *LatencyHistogram) Quantile(q float64) float64 {
	if q <= 0 {
		q = 0
	}
	if q >= 1 {
		q = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(h.total) * q))
	if target == 0 {
		target = 1
	}
	var cumulative uint64
	var prevBound float64
	for i, count := range h.counts {
		cumulative += count
		bound := h.bounds[i]
		if cumulative >= target {
			if count == 0 {
				return bound
			}
			lower := prevBound
			if i == 0 {
				lower = 0
			}
			bucketStart := cumulative - count
			posInBucket := float64(target-bucketStart) / float64(count)
			return lower + (bound-lower)*posInBucket
		}
		prevBound = bound
	}
	return h.bounds[len(h.bounds)-1]
}
