package scheduler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/state"
)

// Scheduler periodically reconciles and expires policies.
type Scheduler struct {
	store          *state.Store
	enforcer       enforcer.Enforcer
	interval       time.Duration
	maxBackoff     time.Duration
	currentBackoff time.Duration
	logger         *zap.Logger
	metrics        *metrics.Recorder
	killSwitch     *control.KillSwitch
}

// New creates a scheduler.
func New(store *state.Store, backend enforcer.Enforcer, interval, maxBackoff time.Duration, kill *control.KillSwitch, logger *zap.Logger, recorder *metrics.Recorder) *Scheduler {
	return &Scheduler{
		store:      store,
		enforcer:   backend,
		interval:   interval,
		maxBackoff: maxBackoff,
		killSwitch: kill,
		logger:     logger,
		metrics:    recorder,
	}
}

// Run starts the reconciliation loop.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		wait := s.nextInterval()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		timer.Stop()

		if s.currentBackoff > 0 {
			if s.metrics != nil {
				s.metrics.ObserveBackoff("scheduler", s.currentBackoff)
			}
			if s.logger != nil {
				s.logger.Debug("scheduler backoff", zap.Duration("backoff", s.currentBackoff))
			}
			backoffTimer := time.NewTimer(s.currentBackoff)
			select {
			case <-ctx.Done():
				backoffTimer.Stop()
				return
			case <-backoffTimer.C:
			}
		}

		if s.killSwitch != nil && s.killSwitch.Enabled() {
			if s.logger != nil {
				s.logger.Debug("scheduler skipped due to kill switch")
			}
			continue
		}

		if err := s.reconcile(ctx); err != nil {
			if s.logger != nil {
				s.logger.Warn("scheduler reconcile encountered errors", zap.Error(err))
			}
			s.bumpBackoff()
		} else {
			s.resetBackoff()
		}
	}
}

func (s *Scheduler) nextInterval() time.Duration {
	interval := s.interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if s.killSwitch != nil && s.killSwitch.Enabled() {
		return interval
	}
	if deadline, ok := s.store.NextPreConsensusDeadline(time.Now().UTC()); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return time.Second
		}
		if remaining < interval {
			if remaining < time.Second {
				return time.Second
			}
			return remaining
		}
	}
	return interval
}

func (s *Scheduler) reconcile(ctx context.Context) error {
	if s.logger != nil {
		s.logger.Debug("scheduler tick")
	}

	var firstErr error
	for _, rec := range s.store.Expired(time.Now().UTC()) {
		if err := s.enforcer.Remove(ctx, rec.Spec.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if s.logger != nil {
				s.logger.Warn("failed to remove expired policy", zap.String("policy_id", rec.Spec.ID), zap.Error(err))
			}
			continue
		}
		if err := s.store.Remove(rec.Spec.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if s.logger != nil {
				s.logger.Error("failed to persist removal", zap.String("policy_id", rec.Spec.ID), zap.Error(err))
			}
			continue
		}
		if s.metrics != nil {
			s.metrics.ObserveRemoved()
			s.metrics.SetActivePolicies(s.store.ActiveCount())
		}
		if s.logger != nil {
			s.logger.Info("expired policy removed", zap.String("policy_id", rec.Spec.ID), zap.String("reason", rec.Reason))
		}
	}
	return firstErr
}

func (s *Scheduler) bumpBackoff() {
	base := s.interval
	if base <= 0 {
		base = 5 * time.Second
	}
	if s.currentBackoff == 0 {
		if s.maxBackoff > 0 && base > s.maxBackoff {
			s.currentBackoff = s.maxBackoff
		} else {
			s.currentBackoff = base
		}
		return
	}
	next := s.currentBackoff * 2
	if s.maxBackoff > 0 && next > s.maxBackoff {
		next = s.maxBackoff
	}
	s.currentBackoff = next
}

func (s *Scheduler) resetBackoff() {
	if s.currentBackoff != 0 && s.metrics != nil {
		s.metrics.ObserveBackoff("scheduler", 0)
	}
	s.currentBackoff = 0
}
