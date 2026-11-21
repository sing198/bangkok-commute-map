package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration for the API server and CLI tools.
type Config struct {
	DatabaseURL     string
	RedisURL        string
	RedisRestURL    string
	RedisRestToken  string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	APIPort         string
	CORSAllowOrigin string
}

// Load reads configuration from environment variables with sensible local defaults.
// Supports API_PORT / PORT, standard Redis TCP URLs (REDIS_URL / KV_URL / UPSTASH_REDIS_URL),
// and Upstash HTTP REST credentials (UPSTASH_REDIS_REST_URL / KV_REST_API_URL + matching *_TOKEN),
// including Vercel Marketplace custom-prefixed Redis/KV variables.
func Load() Config {
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		redisDB = 0
	}

	port := firstNonEmpty(os.Getenv("API_PORT"), os.Getenv("PORT"), "8080")
	redisURL := NormalizeRedisURL(resolveRedisURL())
	restURL, restToken := resolveRedisRestCredentials()

	redisAddr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if redisAddr == "" && redisURL == "" && (restURL == "" || restToken == "") && os.Getenv("VERCEL") != "1" {
		redisAddr = "localhost:6379"
	}

	return Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://commute:commute_dev@localhost:5432/bangkok_commute?sslmode=disable"),
		RedisURL:        redisURL,
		RedisRestURL:    restURL,
		RedisRestToken:  restToken,
		RedisAddr:       redisAddr,
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		APIPort:         port,
		CORSAllowOrigin: getEnv("CORS_ALLOW_ORIGIN", "http://localhost:5173"),
	}
}

// NormalizeRedisURL cleans common copy-paste artifacts (quotes, redis-cli command wrappers)
// and upgrades plain redis:// Upstash hosts to rediss:// because Upstash enforces TLS.
func NormalizeRedisURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	if s == "" {
		return ""
	}

	// Handle pasted CLI commands such as: redis-cli --tls -u redis://default:pass@host.upstash.io:6379
	if !strings.HasPrefix(s, "redis://") && !strings.HasPrefix(s, "rediss://") {
		for _, field := range strings.Fields(s) {
			f := strings.Trim(field, `"'`)
			if strings.HasPrefix(f, "redis://") || strings.HasPrefix(f, "rediss://") {
				s = f
				break
			}
		}
	}

	if strings.HasPrefix(s, "redis://") && strings.Contains(strings.ToLower(s), ".upstash.io") {
		s = "rediss://" + strings.TrimPrefix(s, "redis://")
	}
	return s
}

func resolveRedisURL() string {
	if direct := firstNonEmpty(os.Getenv("REDIS_URL"), os.Getenv("KV_URL"), os.Getenv("UPSTASH_REDIS_URL")); direct != "" {
		return direct
	}
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.HasSuffix(k, "_REST_URL") || strings.HasSuffix(k, "_REST_API_URL") {
			continue
		}
		if strings.HasSuffix(k, "_REDIS_URL") || strings.HasSuffix(k, "_KV_URL") {
			return v
		}
	}
	return ""
}

func resolveRedisRestCredentials() (string, string) {
	pairs := [][2]string{
		{"UPSTASH_REDIS_REST_URL", "UPSTASH_REDIS_REST_TOKEN"},
		{"KV_REST_API_URL", "KV_REST_API_TOKEN"},
	}
	for _, pair := range pairs {
		u := strings.Trim(strings.TrimSpace(os.Getenv(pair[0])), `"'`)
		t := strings.Trim(strings.TrimSpace(os.Getenv(pair[1])), `"'`)
		if u != "" && t != "" {
			return u, t
		}
	}

	// Fallback: support Vercel Marketplace custom prefix (e.g. MYAPP_KV_REST_API_URL + MYAPP_KV_REST_API_TOKEN)
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if !(strings.HasSuffix(k, "_REST_URL") || strings.HasSuffix(k, "_REST_API_URL")) {
			continue
		}
		if !strings.Contains(k, "REDIS") && !strings.Contains(k, "KV") && !strings.Contains(k, "UPSTASH") {
			continue
		}
		tokenKey := strings.TrimSuffix(k, "_URL") + "_TOKEN"
		u := strings.Trim(strings.TrimSpace(v), `"'`)
		t := strings.Trim(strings.TrimSpace(os.Getenv(tokenKey)), `"'`)
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			if t != "" {
				return u, t
			}
		}
	}
	return "", ""
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
