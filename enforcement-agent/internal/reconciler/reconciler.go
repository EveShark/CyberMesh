package reconciler

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/CyberMesh/enforcement-agent/internal/control"
	"github.com/CyberMesh/enforcement-agent/internal/enforcer"
	"github.com/CyberMesh/enforcement-agent/internal/metrics"
	"github.com/CyberMesh/enforcement-agent/internal/policy"
	"github.com/CyberMesh/enforcement-agent/internal/state"
)

// LedgerProvider supplies the authoritative policy view from the ledger/backplane.
type LedgerProvider interface {
	Snapshot(ctx context.Context) (map[string]policy.PolicySpec, error)
}

// Reconciler reapplies and verifies stored policies against the enforcement backend.
type Reconciler struct {
	store          *state.Store
	enforcer       enforcer.Enforcer
	interval       time.Duration
	maxBackoff     time.Duration
	currentBackoff time.Duration
	ledger         LedgerProvider
	driftGrace     time.Duration
	lastLedgerSync time.Time
	logger         *zap.Logger
	metrics        *metrics.Recorder
	killSwitch     *control.KillSwitch
}

// New creates a reconciler.
func New(store *state.Store, backend enforcer.Enforcer, interval, maxBackoff time.Duration, ledger LedgerProvider, driftGrace time.Duration, kill *control.KillSwitch, logger *zap.Logger, metrics *metrics.Recorder) *Reconciler {
	return &Reconciler{
		store:      store,
		enforcer:   backend,
		interval:   interval,
		maxBackoff: maxBackoff,
		ledger:     ledger,
		driftGrace: driftGrace,
		killSwitch: kill,
		logger:     logger,
		metrics:    metrics,
	}
}

// Run starts periodic reconciliation until context cancellation.
func (r *Reconciler) Run(ctx context.Context) {
	for {
		wait := r.nextInterval()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		timer.Stop()

		if r.currentBackoff > 0 {
			if r.metrics != nil {
				r.metrics.ObserveBackoff("reconciler", r.currentBackoff)
			}
			if r.logger != nil {
				r.logger.Debug("reconciler backoff", zap.Duration("backoff", r.currentBackoff))
			}
			backoffTimer := time.NewTimer(r.currentBackoff)
			select {
			case <-ctx.Done():
				backoffTimer.Stop()
				return
			case <-backoffTimer.C:
			}
		}

		if r.killSwitch != nil && r.killSwitch.Enabled() {
			if r.logger != nil {
				r.logger.Debug("reconciler skipped due to kill switch")
			}
			continue
		}

		if err := r.reconcile(ctx); err != nil {
			if r.logger != nil {
				r.logger.Warn("reconciler encountered errors", zap.Error(err))
			}
			r.bumpBackoff()
		} else {
			r.resetBackoff()
		}
	}
}

// RunOnce performs a single reconciliation cycle.
func (r *Reconciler) RunOnce(ctx context.Context) {
	if err := r.reconcile(ctx); err != nil && r.logger != nil {
		r.logger.Warn("reconciler run-once encountered errors", zap.Error(err))
	}
}

func (r *Reconciler) reconcile(ctx context.Context) error {
	if r.killSwitch != nil && r.killSwitch.Enabled() {
		return nil
	}

	now := time.Now().UTC()
	var firstErr error

	if r.ledger != nil && (r.driftGrace <= 0 || now.Sub(r.lastLedgerSync) >= r.driftGrace) {
		snapshot, err := r.ledger.Snapshot(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			r.logWarn("ledger snapshot failed", "", err)
		} else {
			removed, added, reconcileErr := r.store.ReconcileLedger(snapshot, now)
			if reconcileErr != nil {
				if firstErr == nil {
					firstErr = reconcileErr
				}
				r.logWarn("store ledger reconciliation failed", "", reconcileErr)
			} else {
				if len(removed) > 0 {
					for _, rec := range removed {
						if err := r.enforcer.Remove(ctx, rec.Spec.ID); err != nil {
							if firstErr == nil {
								firstErr = err
							}
							r.logWarn("ledger remove", rec.Spec.ID, err)
						}
					}
				}
				if len(added) > 0 {
					for _, spec := range added {
						start := time.Now()
						if err := r.enforcer.Apply(ctx, spec); err != nil {
							if firstErr == nil {
								firstErr = err
							}
							r.logWarn("ledger apply", spec.ID, err)
							if remErr := r.store.Remove(spec.ID); remErr != nil {
								r.logWarn("ledger rollback remove", spec.ID, remErr)
								if firstErr == nil {
									firstErr = remErr
								}
							}
							if r.metrics != nil {
								r.metrics.ObserveReconcileError(time.Since(start))
							}
							continue
						}
						if r.metrics != nil {
							r.metrics.ObserveApplied(time.Since(start))
						}
					}
				}
				if r.metrics != nil {
					r.metrics.ObserveLedgerDrift(len(removed), len(added))
					r.metrics.SetActivePolicies(r.store.ActiveCount())
				}
			}
			r.lastLedgerSync = now
		}
	}

	records := r.store.List()
	preConsensus := make([]state.Record, 0)
	remaining := make([]state.Record, 0)
	for _, rec := range records {
		if !rec.PreConsensus.IsZero() && now.Before(rec.PreConsensus) {
			preConsensus = append(preConsensus, rec)
		} else {
			remaining = append(remaining, rec)
		}
	}

	ordered := append(preConsensus, remaining...)

	for _, rec := range ordered {
		if !rec.ExpiresAt.IsZero() && now.After(rec.ExpiresAt) {
			if err := r.enforcer.Remove(ctx, rec.Spec.ID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				r.logWarn("reconcile remove expired", rec.Spec.ID, err)
				continue
			}
			if err := r.store.Remove(rec.Spec.ID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				r.logWarn("reconcile persist remove", rec.Spec.ID, err)
			}
			if r.metrics != nil {
				r.metrics.ObserveRemoved()
				r.metrics.SetActivePolicies(r.store.ActiveCount())
			}
			if r.logger != nil {
				r.logger.Info("reconciler removed policy", zap.String("policy_id", rec.Spec.ID), zap.String("reason", rec.Reason))
			}
			continue
		}

		start := time.Now()
		if err := r.enforcer.Apply(ctx, rec.Spec); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			r.logWarn("reconcile apply", rec.Spec.ID, err)
			if r.metrics != nil {
				r.metrics.ObserveReconcileError(time.Since(start))
			}
			continue
		}
		if r.metrics != nil {
			r.metrics.ObserveReconciled(time.Since(start))
		}
	}

	return firstErr
}

func (r *Reconciler) logWarn(msg, policyID string, err error) {
	if r.logger != nil {
		r.logger.Warn(msg, zap.String("policy_id", policyID), zap.Error(err))
	}
}

func (r *Reconciler) bumpBackoff() {
	base := r.interval
	if base <= 0 {
		base = 30 * time.Second
	}
	if r.currentBackoff == 0 {
		if r.maxBackoff > 0 && base > r.maxBackoff {
			r.currentBackoff = r.maxBackoff
		} else {
			r.currentBackoff = base
		}
		return
	}
	next := r.currentBackoff * 2
	if r.maxBackoff > 0 && next > r.maxBackoff {
		next = r.maxBackoff
	}
	r.currentBackoff = next
}

func (r *Reconciler) resetBackoff() {
	if r.currentBackoff != 0 && r.metrics != nil {
		r.metrics.ObserveBackoff("reconciler", 0)
	}
	r.currentBackoff = 0
}

func (r *Reconciler) nextInterval() time.Duration {
	interval := r.interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if r.killSwitch != nil && r.killSwitch.Enabled() {
		return interval
	}
	if deadline, ok := r.store.NextPreConsensusDeadline(time.Now().UTC()); ok {
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
