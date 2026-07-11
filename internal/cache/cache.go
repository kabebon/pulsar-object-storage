// Package cache wraps the Redis client with high-level helpers used by the
// session middleware, rate-limiter and quota counters. All keys are namespaced
// under a fixed prefix to avoid collisions when Redis is shared.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"pulsar/internal/config"
)

// Client is the shared Redis wrapper.
type Client struct {
	rdb    *redis.Client
	prefix string
}

// New opens a Redis connection and verifies it with a PING.
func New(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &Client{rdb: rdb, prefix: "pulsar:"}, nil
}

// Close releases the underlying Redis connection.
func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

// Raw exposes the underlying *redis.Client for advanced callers.
func (c *Client) Raw() *redis.Client { return c.rdb }

// key builds a namespaced key.
func (c *Client) key(k string) string { return c.prefix + k }

// Set serializes value via JSON with an expiration. Strings and byte slices are
// stored as-is to avoid double encoding.
func (c *Client) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	v, err := encodeValue(value)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, c.key(key), v, ttl).Err()
}

// encodeValue converts arbitrary values into something redis can store.
func encodeValue(value any) (any, error) {
	switch v := value.(type) {
	case string, []byte, int, int64, float64, bool, nil:
		return v, nil
	}
	b, err := encodeJSON(value)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Get returns the raw string value for a key. Empty string + nil error means
// the key does not exist.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	v, err := c.rdb.Get(ctx, c.key(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// Del removes a key. Missing keys are not an error.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	namespaced := make([]string, len(keys))
	for i, k := range keys {
		namespaced[i] = c.key(k)
	}
	return c.rdb.Del(ctx, namespaced...).Err()
}
