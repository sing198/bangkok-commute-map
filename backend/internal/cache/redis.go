package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Options configures a Redis cache client using either Upstash REST API or standard TCP/TLS Redis.
type Options struct {
	URL       string
	RestURL   string
	RestToken string
	Addr      string
	Password  string
	DB        int
}

// Client wraps go-redis (TCP/TLS) or Upstash Redis REST API with graceful degradation when unavailable.
type Client struct {
	rdb        *redis.Client
	restURL    string
	restToken  string
	httpClient *http.Client
}

// NewFromOptions creates a Redis client from Options.
// When Upstash REST credentials are present, it uses Upstash's stateless HTTP REST API
// (ideal for Vercel serverless functions). Otherwise it falls back to standard TCP/TLS Redis.
func NewFromOptions(opts Options) *Client {
	restURL := strings.TrimRight(strings.TrimSpace(opts.RestURL), "/")
	restToken := strings.TrimSpace(opts.RestToken)
	if restURL != "" && restToken != "" {
		return &Client{
			restURL:   restURL,
			restToken: restToken,
			httpClient: &http.Client{
				Timeout: 2500 * time.Millisecond,
			},
		}
	}
	return NewFromURL(opts.URL, opts.Addr, opts.Password, opts.DB)
}

// NewFromURL creates a Redis client from a standard redis:// or rediss:// URL (e.g. Upstash TCP).
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

// New creates a TCP Redis client. If addr is empty or unreachable,
// operations degrade gracefully without failing requests.
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

// Mode returns which Redis transport is configured ("upstash-rest", "redis-tcp", or "disabled").
func (c *Client) Mode() string {
	if c == nil {
		return "disabled"
	}
	if c.restURL != "" && c.restToken != "" {
		return "upstash-rest"
	}
	if c.rdb != nil {
		return "redis-tcp"
	}
	return "disabled"
}

// Ping checks whether Redis is reachable right now.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("redis client not configured")
	}
	if c.restURL != "" && c.restToken != "" {
		res, err := c.doRest(ctx, []any{"PING"})
		if err != nil {
			return err
		}
		var pong string
		if err := json.Unmarshal(res, &pong); err != nil || !strings.EqualFold(pong, "PONG") {
			return fmt.Errorf("unexpected upstash ping result: %s", string(res))
		}
		return nil
	}
	if c.rdb == nil {
		return fmt.Errorf("redis client not configured")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.rdb.Ping(pingCtx).Err()
}

// Get retrieves a cached string value. Returns ("", false) on miss or Redis error.
func (c *Client) Get(ctx context.Context, key string) (string, bool) {
	if c == nil {
		return "", false
	}
	if c.restURL != "" && c.restToken != "" {
		res, err := c.doRest(ctx, []any{"GET", key})
		if err != nil || len(res) == 0 || string(res) == "null" {
			return "", false
		}
		var val string
		if err := json.Unmarshal(res, &val); err != nil {
			return "", false
		}
		return val, true
	}
	if c.rdb == nil {
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
	if c == nil {
		return
	}
	if c.restURL != "" && c.restToken != "" {
		secs := int64(ttl.Seconds())
		if secs <= 0 {
			secs = 1
		}
		_, _ = c.doRest(ctx, []any{"SET", key, value, "EX", secs})
		return
	}
	if c.rdb == nil {
		return
	}
	_ = c.rdb.Set(ctx, key, value, ttl).Err()
}

// DeleteByPrefix removes all keys matching prefix* (used after data pipeline re-seeding).
func (c *Client) DeleteByPrefix(ctx context.Context, prefix string) (int64, error) {
	if c == nil {
		return 0, nil
	}
	if c.restURL != "" && c.restToken != "" {
		return c.deleteByPrefixRest(ctx, prefix)
	}
	if c.rdb == nil {
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

type upstashResponse struct {
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func (c *Client) doRest(ctx context.Context, cmd []any) (json.RawMessage, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.restURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.restToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstash rest http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed upstashResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("decode upstash response: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("upstash redis error: %s", parsed.Error)
	}
	return parsed.Result, nil
}

func (c *Client) deleteByPrefixRest(ctx context.Context, prefix string) (int64, error) {
	cursor := "0"
	var deleted int64
	pattern := prefix + "*"
	for {
		res, err := c.doRest(ctx, []any{"SCAN", cursor, "MATCH", pattern, "COUNT", 100})
		if err != nil {
			return deleted, err
		}
		var pair []json.RawMessage
		if err := json.Unmarshal(res, &pair); err != nil || len(pair) != 2 {
			return deleted, fmt.Errorf("unexpected upstash scan response: %s", string(res))
		}
		var nextCursor string
		if err := json.Unmarshal(pair[0], &nextCursor); err != nil {
			return deleted, err
		}
		var keys []string
		if err := json.Unmarshal(pair[1], &keys); err != nil {
			return deleted, err
		}
		if len(keys) > 0 {
			delCmd := make([]any, 0, len(keys)+1)
			delCmd = append(delCmd, "DEL")
			for _, k := range keys {
				delCmd = append(delCmd, k)
			}
			delRes, err := c.doRest(ctx, delCmd)
			if err != nil {
				return deleted, err
			}
			var n int64
			if err := json.Unmarshal(delRes, &n); err == nil {
				deleted += n
			}
		}
		cursor = nextCursor
		if cursor == "0" || cursor == "" {
			break
		}
	}
	return deleted, nil
}
