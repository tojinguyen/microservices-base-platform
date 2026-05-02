package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var luaFixedWindow = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
    redis.call("EXPIRE", KEYS[1], ARGV[2])
end
local ttl = redis.call("TTL", KEYS[1])
return {current, ttl}
`)

type Config struct {
	Limit  int
	Window time.Duration
}

type RateLimiter struct {
	client *redis.Client
	cfg    Config
}

type Result struct {
	Allowed   bool
	Remaining int
	ResetIn   time.Duration
}

func New(client *redis.Client, cfg Config) *RateLimiter {
	return &RateLimiter{client: client, cfg: cfg}
}

func (r *RateLimiter) Allow(ctx context.Context, key string) (Result, error) {
	return r.allow(ctx, key, r.cfg.Limit, r.cfg.Window)
}

func (r *RateLimiter) allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	windowSecs := int64(window.Seconds())

	vals, err := luaFixedWindow.Run(ctx, r.client, []string{key}, limit, windowSecs).Slice()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: redis error: %w", err)
	}

	current := vals[0].(int64)
	ttl := vals[1].(int64)
	if ttl < 0 {
		ttl = windowSecs
	}

	remaining := int64(limit) - current
	if remaining < 0 {
		remaining = 0
	}

	return Result{
		Allowed:   current <= int64(limit),
		Remaining: int(remaining),
		ResetIn:   time.Duration(ttl) * time.Second,
	}, nil
}
