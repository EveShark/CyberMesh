package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const reserveScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local expire = tonumber(ARGV[5])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local count = redis.call('ZCARD', key)
if count >= limit then
  return {0, count}
end
redis.call('ZADD', key, now, member)
if expire > 0 then
  redis.call('PEXPIRE', key, expire)
end
return {1, count + 1}
`

type redisCoordinator struct {
	client   RedisClient
	prefix   string
	sha      string
	shaMutex sync.Mutex
}

type redisReservation struct {
	client RedisClient
	key    string
	member string
	count  int64
}

func newRedis(opts RedisOptions) (Coordinator, error) {
	if opts.Client == nil {
		return nil, fmt.Errorf("ratelimit: redis client required")
	}
	return &redisCoordinator{client: opts.Client, prefix: opts.KeyPrefix}, nil
}

func (r *redisCoordinator) Reserve(ctx context.Context, scope string, window time.Duration, limit int64, now time.Time) (Reservation, error) {
	key := r.key(scope)
	args := []any{now.UnixMilli(), window.Milliseconds(), limit, uuid.NewString(), (window * 2).Milliseconds()}
	result, err := r.eval(ctx, key, args...)
	if err != nil {
		return nil, err
	}
	reply, ok := result.([]any)
	if !ok || len(reply) != 2 {
		return nil, fmt.Errorf("ratelimit: unexpected redis response %v", result)
	}
	allowed, _ := toInt64(reply[0])
	count, _ := toInt64(reply[1])
	if allowed == 0 {
		return nil, ErrRateLimitExceeded
	}
	member := args[3].(string)
	return &redisReservation{client: r.client, key: key, member: member, count: count}, nil
}

func (r *redisCoordinator) key(scope string) string {
	if r.prefix == "" {
		return fmt.Sprintf("rl:%s", scope)
	}
	return fmt.Sprintf("%s:%s", r.prefix, scope)
}

func (r *redisCoordinator) eval(ctx context.Context, key string, args ...any) (any, error) {
	sha := r.loadScript(ctx)
	if sha == "" {
		return r.client.Eval(ctx, reserveScript, []string{key}, args...)
	}
	res, err := r.client.EvalSha(ctx, sha, []string{key}, args...)
	if err != nil {
		return r.client.Eval(ctx, reserveScript, []string{key}, args...)
	}
	return res, nil
}

func (r *redisCoordinator) loadScript(ctx context.Context) string {
	r.shaMutex.Lock()
	defer r.shaMutex.Unlock()
	if r.sha != "" {
		return r.sha
	}
	sha, err := r.client.ScriptLoad(ctx, reserveScript)
	if err != nil {
		return ""
	}
	r.sha = sha
	return sha
}

func (r *redisReservation) Commit(context.Context) error {
	return nil
}

func (r *redisReservation) Release(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
	_, err := r.client.ZRem(ctx, r.key, r.member)
	return err
}

func (r *redisReservation) Count() int64 {
	return r.count
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case uint64:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		var n int64
		_, err := fmt.Sscan(v, &n)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}
