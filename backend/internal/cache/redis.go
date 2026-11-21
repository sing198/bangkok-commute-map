package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client wraps go-redis with graceful degradation when Redis is unavailable.
type Client struct {
	rdb *redis.Client
}

// NewFromURL creates a Redis client from a standard redis:// or rediss:// URL (e.g. Upstash).
// Falls back to addr/password/db when rawURL is empty.
func NewFromURL(rawURL, addr, password string, db int) *Client {
	if rawURL != "" {
		opts, err := redis.ParseURL(rawURL)
		if err == nil {
			opts.DialTimeout = 2 * time.Second
			opts.ReadTimeout = 2 * time.Second
			opts.WriteTimeout = 2 * time.Second
			return &Client{rdb: redis.NewClient(opts)}
		}
	}
	return New(addr, password, db)
}

// New creates a Redis client and pings it once. If Redis is unreachable,
// it still returns a Client whose operations degrade gracefully without failing requests.
func New(addr, password string, db int) *Client {
	if addr == "" {
		return &Client{}
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	return &Client{rdb: rdb}
}

// Ping checks whether Redis is reachable right now.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return fmt.Errorf("redis client not configured")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.rdb.Ping(pingCtx).Err()
}

// Get retrieves a cached string value. Returns ("", false) on miss or Redis error.
func (c *Client) Get(ctx context.Context, key string) (string, bool) {
	if c == nil || c.rdb == nil {
		return "", false
	}
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

// Set stores a value with TTL. Errors are silently ignored to preserve graceful fallback.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	_ = c.rdb.Set(ctx, key, value, ttl).Err()
}

// DeleteByPrefix removes all keys matching prefix* (used after data pipeline re-seeding).
func (c *Client) DeleteByPrefix(ctx context.Context, prefix string) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	var (
		cursor  uint64
		deleted int64
	)
	pattern := prefix + "*"
	for {
		keys, nextCursor, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			n, err := c.rdb.Del(ctx, keys...).Result()
			if err != nil {
				return deleted, err
			}
			deleted += n
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return deleted, nil
}

// Close shuts down the underlying Redis connection pool.
func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}
