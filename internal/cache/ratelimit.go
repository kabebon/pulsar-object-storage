package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a fixed-window counter per (key, window) using a
// single Redis INCR with TTL. It is intentionally simple and atomic; for
// smoother limits a sliding window can be layered on later.
type RateLimiter struct {
	c *Client
}

// NewRateLimiter builds a limiter backed by the given cache client.
func NewRateLimiter(c *Client) *RateLimiter { return &RateLimiter{c: c} }

// Result reports whether the action was allowed and how long to wait otherwise.
type Result struct {
	Allowed   bool
	Limit     int64
	Remaining int64
	RetryAfter time.Duration
}

// Allow reports whether an event identified by key may proceed within (limit,
// window). The first call seeds the counter and sets the TTL.
func (r *RateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (Result, error) {
	full := r.c.key("rl:" + key)
	// Atomic check-and-increment using Lua so multi-instance deployments agree.
	res, err := fixedWindowScript.Run(ctx, r.c.rdb, []string{full}, limit, window.Milliseconds()).Result()
	if err != nil {
		// Fail open: never block purely on Redis errors.
		return Result{Allowed: true, Limit: limit, Remaining: limit - 1}, nil
	}
	vals, ok := res.([]any)
	if !ok || len(vals) < 3 {
		return Result{Allowed: true, Limit: limit, Remaining: limit - 1}, nil
	}
	count := toInt64(vals[0])
	ttlMS := toInt64(vals[1])
	allowed := count <= limit
	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}
	retry := time.Duration(ttlMS) * time.Millisecond
	return Result{Allowed: allowed, Limit: limit, Remaining: remaining, RetryAfter: retry}, nil
}

// fixedWindowScript increments a key and applies a TTL only on first creation.
// Returns: {current_count, ttl_ms_in_ms, limit}.
var fixedWindowScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local current = redis.call("INCR", key)
local ttl = window
if current == 1 then
  redis.call("PEXPIRE", key, window)
else
  ttl = redis.call("PTTL", key)
  if ttl < 0 then ttl = window end
end
return {current, ttl, limit}
`)

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
