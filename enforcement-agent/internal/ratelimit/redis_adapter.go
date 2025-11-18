package ratelimit

import (
	"context"

	redis "github.com/redis/go-redis/v9"
)

// NewRedisAdapter wraps a go-redis client to satisfy RedisClient.
func NewRedisAdapter(client redis.UniversalClient) RedisClient {
	return &redisAdapter{client: client}
}

type redisAdapter struct {
	client redis.UniversalClient
}

func (r *redisAdapter) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	cmd := r.client.Eval(ctx, script, keys, args...)
	return cmd.Result()
}

func (r *redisAdapter) EvalSha(ctx context.Context, sha1 string, keys []string, args ...any) (any, error) {
	cmd := r.client.EvalSha(ctx, sha1, keys, args...)
	return cmd.Result()
}

func (r *redisAdapter) ScriptLoad(ctx context.Context, script string) (string, error) {
	cmd := r.client.ScriptLoad(ctx, script)
	return cmd.Result()
}

func (r *redisAdapter) ZRem(ctx context.Context, key string, members ...any) (int64, error) {
	cmd := r.client.ZRem(ctx, key, members...)
	return cmd.Result()
}
