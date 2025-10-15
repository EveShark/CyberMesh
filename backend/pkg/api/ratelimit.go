package api

import (
	"sync"
	"time"

	"backend/pkg/utils"
)

// RateLimiterConfig holds rate limiter configuration
type RateLimiterConfig struct {
	RequestsPerMinute int
	Burst             int
	Logger            *utils.Logger
}

// RateLimiter implements token bucket rate limiting
type RateLimiter struct {
	config  RateLimiterConfig
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
}

// tokenBucket represents a token bucket for a client
type tokenBucket struct {
	tokens   float64
	lastTime time.Time
	capacity float64
	refill   float64 // tokens per second
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimiterConfig) *RateLimiter {
	return &RateLimiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
	}
}

// Allow checks if a request is allowed
func (rl *RateLimiter) Allow(clientID string) (bool, int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create bucket
	bucket, exists := rl.buckets[clientID]
	if !exists {
		bucket = &tokenBucket{
			tokens:   float64(rl.config.RequestsPerMinute),
			lastTime: now,
			capacity: float64(rl.config.RequestsPerMinute),
			refill:   float64(rl.config.RequestsPerMinute) / 60.0, // per second
		}
		rl.buckets[clientID] = bucket
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(bucket.lastTime).Seconds()
	bucket.tokens += elapsed * bucket.refill
	if bucket.tokens > bucket.capacity {
		bucket.tokens = bucket.capacity
	}
	bucket.lastTime = now

	// Check if request allowed
	if bucket.tokens >= 1.0 {
		bucket.tokens -= 1.0
		
		// Calculate reset time (when bucket will be full)
		secondsUntilFull := (bucket.capacity - bucket.tokens) / bucket.refill
		resetTime := now.Add(time.Duration(secondsUntilFull) * time.Second).Unix()
		
		return true, resetTime
	}

	// Calculate when next token available
	secondsUntilToken := (1.0 - bucket.tokens) / bucket.refill
	resetTime := now.Add(time.Duration(secondsUntilToken) * time.Second).Unix()

	return false, resetTime
}

// GetMetrics returns rate limiter metrics
func (rl *RateLimiter) GetMetrics() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	return map[string]interface{}{
		"tracked_clients": len(rl.buckets),
		"requests_per_minute": rl.config.RequestsPerMinute,
		"burst": rl.config.Burst,
	}
}

// Cleanup removes old buckets (call periodically)
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for clientID, bucket := range rl.buckets {
		if now.Sub(bucket.lastTime) > maxAge {
			delete(rl.buckets, clientID)
		}
	}
}
