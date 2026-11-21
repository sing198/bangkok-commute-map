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
// Supports both API_PORT and cloud-provider PORT, as well as REDIS_URL (e.g. Upstash).
func Load() Config {
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		redisDB = 0
	}

	port := os.Getenv("API_PORT")
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	return Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://commute:commute_dev@localhost:5432/bangkok_commute?sslmode=disable"),
		RedisURL:        os.Getenv("REDIS_URL"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
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
