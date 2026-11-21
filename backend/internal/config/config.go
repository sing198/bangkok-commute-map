package config

import (
	"os"
	"strconv"
)

// Config holds runtime configuration for the API server and CLI tools.
type Config struct {
	DatabaseURL     string
	RedisURL        string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	APIPort         string
	CORSAllowOrigin string
}

// Load reads configuration from environment variables with sensible local defaults.
// Supports both API_PORT and cloud-provider PORT, as well as REDIS_URL / KV_URL (Upstash / Vercel Storage).
func Load() Config {
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		redisDB = 0
	}

	port := firstNonEmpty(os.Getenv("API_PORT"), os.Getenv("PORT"), "8080")
	redisURL := firstNonEmpty(os.Getenv("REDIS_URL"), os.Getenv("KV_URL"), os.Getenv("UPSTASH_REDIS_URL"))

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" && redisURL == "" && os.Getenv("VERCEL") != "1" {
		redisAddr = "localhost:6379"
	}

	return Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://commute:commute_dev@localhost:5432/bangkok_commute?sslmode=disable"),
		RedisURL:        redisURL,
		RedisAddr:       redisAddr,
		RedisPassword:   os.Getenv("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		APIPort:         port,
		CORSAllowOrigin: getEnv("CORS_ALLOW_ORIGIN", "http://localhost:5173"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
