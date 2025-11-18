package ratelimit

import (
	"context"
	"time"
)

type localCoordinator struct {
	counter ScopedCounter
}

type localReservation struct{}

func newLocal(opts LocalOptions) Coordinator {
	return &localCoordinator{counter: opts.Counter}
}

func (l *localCoordinator) Reserve(_ context.Context, scope string, window time.Duration, limit int64, now time.Time) (Reservation, error) {
	if l.counter == nil {
		return nil, ErrRateLimitExceeded
	}
	var current int
	if scope == "" {
		current = l.counter.RecentCount(window, now)
	} else {
		current = l.counter.RecentCountFor(scope, window, now)
	}
	if int64(current) >= limit {
		return nil, ErrRateLimitExceeded
	}
	return localReservation{}, nil
}

func (localReservation) Commit(context.Context) error { return nil }

func (localReservation) Release(context.Context) error { return nil }

func (localReservation) Count() int64 { return 0 }
