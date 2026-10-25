package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Server — параметры HTTP-сервера дашборда.
type Server struct {
	Port string
}

// Gateway — подключение к StorageGateway (gRPC-контракт).
type Gateway struct {
	Addr    string        // host:port,
	Timeout time.Duration // таймаут на один RPC
}

// Projection — параметры in-memory проекции
type Projection struct {
	MaxDays            int
	RefreshInterval    time.Duration   //период для рефреш
	StartupRetryDelays []time.Duration //список с ретраями
}

type Config struct {
	Server     Server
	Gateway    Gateway
	Projection Projection
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	cfg := &Config{
		Server: Server{
			Port: getEnv("CLICK_DASHBOARD_PORT", "8085"),
		},
		Gateway: Gateway{
			Addr:    getEnv("CLICK_DASHBOARD_STORAGE_GATEWAY_ADDR", "localhost:9084"),
			Timeout: getEnvDuration("CLICK_DASHBOARD_STORAGE_GATEWAY_TIMEOUT", 10*time.Second),
		},
		Projection: Projection{
			MaxDays:         getEnvMaxDays("CLICK_DASHBOARD_MAX_DAYS", 30),
			RefreshInterval: getEnvDuration("CLICK_DASHBOARD_REFRESH_INTERVAL", 5*time.Minute),
			StartupRetryDelays: getEnvDurationSlice("CLICK_DASHBOARD_STARTUP_RETRY_DELAYS",
				[]time.Duration{10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second, 10 * time.Second}),
		},
	}

	if cfg.Projection.RefreshInterval <= 0 {
		log.Fatalf("config: CLICK_DASHBOARD_REFRESH_INTERVAL must be > 0, got %s", cfg.Projection.RefreshInterval)
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

// getEnvDurationSlice парсит список длительностей через запятую: "10s,10s,20s".
// Некорректный/неположительный элемент или пустое значение → fallback целиком.
func getEnvDurationSlice(key string, fallback []time.Duration) []time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]time.Duration, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d, err := time.ParseDuration(p)
		if err != nil {
			log.Printf("config: invalid duration in %s=%q, using default %v", key, v, fallback)
			return fallback
		}
		if d <= 0 {
			log.Printf("config: non-positive duration in %s=%q, using default %v", key, v, fallback)
			return fallback
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
