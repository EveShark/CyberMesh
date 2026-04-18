package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type endpointCircuitBreaker struct {
	mu             sync.Mutex
	failureCount   int
	openUntil      time.Time
	errorThreshold int
	cooldown       time.Duration
}

func newEndpointCircuitBreaker(errorThreshold int, cooldown time.Duration) *endpointCircuitBreaker {
	if errorThreshold <= 0 {
		errorThreshold = 5
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Second
	}
	return &endpointCircuitBreaker{errorThreshold: errorThreshold, cooldown: cooldown}
}

func (b *endpointCircuitBreaker) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.openUntil.IsZero() && now.Before(b.openUntil) {
		return false
	}
	if !b.openUntil.IsZero() && !now.Before(b.openUntil) {
		b.openUntil = time.Time{}
		b.failureCount = 0
	}
	return true
}

func (b *endpointCircuitBreaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount = 0
	b.openUntil = time.Time{}
}

func (b *endpointCircuitBreaker) recordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount++
	if b.failureCount >= b.errorThreshold {
		b.openUntil = now.Add(b.cooldown)
		b.failureCount = 0
	}
}

type controlBreakerRegistry struct {
	mu       sync.Mutex
	breakers map[string]*endpointCircuitBreaker
	cfgErrs  int
	cfgCd    time.Duration
}

func newControlBreakerRegistry(threshold int, cooldown time.Duration) *controlBreakerRegistry {
	return &controlBreakerRegistry{
		breakers: make(map[string]*endpointCircuitBreaker),
		cfgErrs:  threshold,
		cfgCd:    cooldown,
	}
}

func (r *controlBreakerRegistry) get(name string) *endpointCircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.breakers[name]; ok {
		return b
	}
	b := newEndpointCircuitBreaker(r.cfgErrs, r.cfgCd)
	r.breakers[name] = b
	return b
}

func (s *Server) shouldUseControlBreaker() bool {
	return s != nil && s.config != nil && s.config.ControlAPIBreakerEnabled && s.controlBreakers != nil
}

func (s *Server) checkControlBreaker(endpoint string) error {
	if !s.shouldUseControlBreaker() {
		return nil
	}
	b := s.controlBreakers.get(endpoint)
	if b.allow(time.Now().UTC()) {
		return nil
	}
	s.controlBreakerOpenTotal.Add(1)
	return fmt.Errorf("circuit breaker open")
}

func (s *Server) recordControlBreakerSuccess(endpoint string) {
	if !s.shouldUseControlBreaker() {
		return
	}
	s.controlBreakers.get(endpoint).recordSuccess()
}

func (s *Server) recordControlBreakerFailure(endpoint string) {
	if !s.shouldUseControlBreaker() {
		return
	}
	s.controlBreakers.get(endpoint).recordFailure(time.Now().UTC())
}

func (s *Server) controlMutationTimeout() time.Duration {
	if s != nil && s.config != nil && s.config.ControlMutationTimeout > 0 {
		return s.config.ControlMutationTimeout
	}
	return 10 * time.Second
}

func (s *Server) controlTraceTimeout() time.Duration {
	if s != nil && s.config != nil && s.config.ControlTraceTimeout > 0 {
		return s.config.ControlTraceTimeout
	}
	return 15 * time.Second
}

func (s *Server) controlReadTimeout() time.Duration {
	if s != nil && s.config != nil && s.config.ControlReadTimeout > 0 {
		return s.config.ControlReadTimeout
	}
	return 5 * time.Second
}

func (s *Server) noteControlTimeout(err error, mutation bool) {
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.controlAPITimeoutTotal.Add(1)
		if mutation {
			s.controlMutationTimeoutTotal.Add(1)
		}
	}
}

func (s *Server) enforceMutationThrottle(actor, targetKey string) *controlMutationGateError {
	if s == nil || s.config == nil {
		return nil
	}
	if s.controlMutationLimiter != nil {
		allowed, _ := s.controlMutationLimiter.Allow(actor)
		if !allowed {
			s.controlMutationRateLimitedTotal.Add(1)
			return &controlMutationGateError{
				Code:       "CONTROL_MUTATION_RATE_LIMITED",
				Message:    "mutation rate limit exceeded",
				HTTPStatus: 429,
			}
		}
	}
	cooldown := s.config.ControlMutationCooldown
	if cooldown <= 0 || targetKey == "" {
		return nil
	}
	now := time.Now().UTC()
	s.controlMutationLastActionMu.Lock()
	last := s.controlMutationLastActionByTarget[targetKey]
	if !last.IsZero() && now.Sub(last) < cooldown {
		s.controlMutationLastActionMu.Unlock()
		s.controlMutationCooldownBlockedTotal.Add(1)
		return &controlMutationGateError{
			Code:       "CONTROL_MUTATION_COOLDOWN_ACTIVE",
			Message:    "mutation cooldown active for target",
			HTTPStatus: 429,
		}
	}
	s.controlMutationLastActionByTarget[targetKey] = now
	s.controlMutationLastActionMu.Unlock()
	return nil
}
