package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort string

	// StorageGateway (gRPC).
	StorageGatewayAddr    string        // host:port, без схемы
	StorageGatewayTimeout time.Duration // таймаут на один RPC

	// Лимит дней в проекции и период её обновления.
	MaxDays         int
	RefreshInterval time.Duration
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	cfg := &Config{
		ServerPort: getEnv("PORT_CLICK_DASHBOARD", "8085"),

		StorageGatewayAddr:    getEnv("STORAGE_GATEWAY_ADDR", "localhost:8084"),
		StorageGatewayTimeout: getEnvDuration("STORAGE_GATEWAY_TIMEOUT", 10*time.Second),

		MaxDays:         getEnvMaxDays("MAX_DAYS", 30),
		RefreshInterval: getEnvDuration("REFRESH_INTERVAL", 5*time.Minute),
	}

	if cfg.RefreshInterval <= 0 {
		log.Fatalf("config: REFRESH_INTERVAL must be > 0, got %s", cfg.RefreshInterval)
	}

	return cfg
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvMaxDays(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("config: invalid int for %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	if n < 1 {
		log.Fatalf("config: %s must be >= 1, got %d", key, n)
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("config: invalid duration for %s=%q, using default %s", key, v, fallback)
		return fallback
	}
	return d
}
