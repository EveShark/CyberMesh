package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrRateLimitExceeded indicates the rate limit was hit.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// Reservation represents a reserved slot in the rate limiter.
type Reservation interface {
	Commit(ctx context.Context) error
	Release(ctx context.Context) error
	Count() int64
}

// Coordinator coordinates policy rate limiting across agents.
type Coordinator interface {
	Reserve(ctx context.Context, scope string, window time.Duration, limit int64, now time.Time) (Reservation, error)
}

// Factory constructs a Coordinator based on backend identifier.
func Factory(backend string, opts Options) (Coordinator, error) {
	switch backend {
	case "", "local":
		if opts.Local == nil {
			return nil, fmt.Errorf("ratelimit: local backend requires options")
		}
		return newLocal(*opts.Local), nil
	case "redis":
		if opts.Redis == nil {
			return nil, fmt.Errorf("ratelimit: redis backend requires configuration")
		}
		return newRedis(*opts.Redis)
	default:
		return nil, fmt.Errorf("ratelimit: unsupported backend %s", backend)
	}
}

// Options groups coordinator configuration.
type Options struct {
	Local *LocalOptions
	Redis *RedisOptions
}

// LocalOptions configure the in-process coordinator.
type LocalOptions struct {
	Counter ScopedCounter
}

// ScopedCounter exposes store history counts for scoped evaluation.
type ScopedCounter interface {
	RecentCount(window time.Duration, now time.Time) int
	RecentCountFor(scope string, window time.Duration, now time.Time) int
}

// RedisOptions configure Redis coordinator.
type RedisOptions struct {
	Client    RedisClient
	KeyPrefix string
}

// RedisClient defines minimal Redis operations required.
type RedisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
	EvalSha(ctx context.Context, sha1 string, keys []string, args ...any) (any, error)
	ScriptLoad(ctx context.Context, script string) (string, error)
	ZRem(ctx context.Context, key string, members ...any) (int64, error)
}
