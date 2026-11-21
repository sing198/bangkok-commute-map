package config

import "testing"

func TestNormalizeRedisURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain local redis",
			in:   "redis://localhost:6379/0",
			want: "redis://localhost:6379/0",
		},
		{
			name: "upstash plain scheme upgraded to rediss",
			in:   "redis://default:secret@exact-koi-12345.upstash.io:6379",
			want: "rediss://default:secret@exact-koi-12345.upstash.io:6379",
		},
		{
			name: "quoted upstash rediss url",
			in:   ` "rediss://default:secret@exact-koi-12345.upstash.io:6379" `,
			want: "rediss://default:secret@exact-koi-12345.upstash.io:6379",
		},
		{
			name: "pasted redis-cli tls command",
			in:   "redis-cli --tls -u redis://default:secret@exact-koi-12345.upstash.io:6379",
			want: "rediss://default:secret@exact-koi-12345.upstash.io:6379",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeRedisURL(tc.in); got != tc.want {
				t.Fatalf("NormalizeRedisURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoadCustomPrefixUpstashRest(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("KV_URL", "")
	t.Setenv("UPSTASH_REDIS_URL", "")
	t.Setenv("UPSTASH_REDIS_REST_URL", "")
	t.Setenv("UPSTASH_REDIS_REST_TOKEN", "")
	t.Setenv("KV_REST_API_URL", "")
	t.Setenv("KV_REST_API_TOKEN", "")
	t.Setenv("VERCEL", "1")
	t.Setenv("COMMUTE_KV_REST_API_URL", "https://example.upstash.io")
	t.Setenv("COMMUTE_KV_REST_API_TOKEN", "tok_123")

	cfg := Load()
	if cfg.RedisRestURL != "https://example.upstash.io" || cfg.RedisRestToken != "tok_123" {
		t.Fatalf("expected custom prefix REST credentials, got url=%q token=%q", cfg.RedisRestURL, cfg.RedisRestToken)
	}
	if cfg.RedisAddr != "" {
		t.Fatalf("expected empty RedisAddr on Vercel with REST creds, got %q", cfg.RedisAddr)
	}
}
