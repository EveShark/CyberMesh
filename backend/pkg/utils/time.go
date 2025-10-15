package utils

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"
)

// Time-related errors
var (
	ErrInvalidDuration    = errors.New("time: invalid duration")
	ErrTimeoutExceeded    = errors.New("time: timeout exceeded")
	ErrMaxRetriesExceeded = errors.New("time: max retries exceeded")
)

// BackoffConfig configures backoff behavior
type BackoffConfig struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	Jitter       float64
	MaxRetries   int
}

// DefaultBackoffConfig returns sensible defaults
func DefaultBackoffConfig() *BackoffConfig {
	return &BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.2,
		MaxRetries:   5,
	}
}

// Jitter applies cryptographically secure jitter to a duration
func Jitter(d time.Duration, factor float64) time.Duration {
	if factor <= 0 {
		return d
	}
	if factor > 1 {
		factor = 1
	}

	// Use crypto/rand for unpredictable jitter
	randomFactor := secureRandomFloat64()

	// Apply symmetric jitter: d * (1 +/- factor*random)
	f := 1.0 + (randomFactor*2-1)*factor
	return time.Duration(float64(d) * f)
}

// ExponentialBackoff computes exponential backoff with secure jitter
func ExponentialBackoff(attempt int, base, max time.Duration, jitter float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	if max <= 0 {
		max = 30 * time.Second
	}

	// Calculate exponential backoff
	backoff := float64(base) * math.Pow(2, float64(attempt))

	// Apply jitter using crypto/rand
	if jitter > 0 {
		if jitter > 1 {
			jitter = 1
		}
		randomFactor := secureRandomFloat64()
		jitterMultiplier := 1.0 + (randomFactor*2-1)*jitter
		backoff = backoff * jitterMultiplier
	}

	// Cap at max
	if backoff > float64(max) {
		backoff = float64(max)
	}

	return time.Duration(backoff)
}

// AdaptiveTimeout adjusts timeout using AIMD
func AdaptiveTimeout(prev, min, max time.Duration, success bool, decrease, increase float64) time.Duration {
	if min <= 0 {
		min = 100 * time.Millisecond
	}
	if max < min {
		max = min
	}
	if prev <= 0 {
		prev = min
	}
	if decrease <= 0 || decrease >= 1 {
		decrease = 0.9
	}
	if increase <= 1 {
		increase = 1.5
	}

	var next float64
	if success {
		// Multiplicative decrease (additive would be too slow)
		next = float64(prev) * decrease
		if next < float64(min) {
			next = float64(min)
		}
	} else {
		// Multiplicative increase
		next = float64(prev) * increase
		if next > float64(max) {
			next = float64(max)
		}
	}

	return time.Duration(next)
}

// SleepWithContext sleeps for duration or until context is canceled
func SleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	Jitter        float64
	Timeout       time.Duration
	RetryableFunc func(error) bool
}

// DefaultRetryConfig returns sensible defaults
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:   5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		Jitter:        0.2,
		Timeout:       5 * time.Minute,
		RetryableFunc: IsRetryable,
	}
}

// RetryContext executes fn with exponential backoff retry
func RetryContext(ctx context.Context, config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	if config.MaxAttempts <= 0 {
		config.MaxAttempts = 1
	}

	// Apply overall timeout if configured
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	var lastErr error
	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		// Check context before attempt
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		default:
		}

		// Execute function
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// Check if error is retryable
		if config.RetryableFunc != nil && !config.RetryableFunc(lastErr) {
			return lastErr
		}

		// Don't sleep on last attempt
		if attempt == config.MaxAttempts-1 {
			break
		}

		// Calculate backoff with secure jitter
		backoff := ExponentialBackoff(attempt, config.InitialDelay, config.MaxDelay, config.Jitter)

		// Sleep with context awareness
		if err := SleepWithContext(ctx, backoff); err != nil {
			return lastErr
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return ErrMaxRetriesExceeded
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	maxFailures  int
	resetTimeout time.Duration

	failures    int
	lastFailure time.Time
	state       CircuitState
	mu          sync.RWMutex
}

// CircuitState represents circuit breaker state
type CircuitState int

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 60 * time.Second
	}

	return &CircuitBreaker{
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		state:        StateClosed,
	}
}

// Execute runs fn through the circuit breaker
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.allowRequest() {
		return NewCircuitOpenError("circuit breaker is open")
	}

	err := fn()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if enough time has passed to try half-open
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}

	return false
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.maxFailures {
		cb.state = StateOpen
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.state = StateClosed
	}

	cb.failures = 0
}

// GetState returns the current circuit state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset manually resets the circuit breaker
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
}

// Helper functions

// secureRandomFloat64 generates a cryptographically secure random float in [0,1)
func secureRandomFloat64() float64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to time-based randomness (not ideal but better than panic)
		return float64(time.Now().UnixNano()%1000) / 1000.0
	}

	// Convert to uint64 and normalize to [0,1)
	n := binary.BigEndian.Uint64(buf[:])
	return float64(n) / float64(math.MaxUint64)
}

// WithTimeout creates a context with timeout
func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// WithDeadline creates a context with deadline
func WithDeadline(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, deadline)
}

// TimeUntil returns duration until a time
func TimeUntil(t time.Time) time.Duration {
	return time.Until(t)
}

// TimeSince returns duration since a time
func TimeSince(t time.Time) time.Duration {
	return time.Since(t)
}

// IsExpired checks if a timestamp has expired
func IsExpired(t time.Time) bool {
	return time.Now().After(t)
}

// IsExpiredWithGracePeriod checks expiration with grace period
func IsExpiredWithGracePeriod(t time.Time, grace time.Duration) bool {
	return time.Now().After(t.Add(grace))
}
