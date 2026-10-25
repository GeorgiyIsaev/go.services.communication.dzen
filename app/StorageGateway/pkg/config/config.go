package config

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type HTTP struct {
	Port string
}
type GRPC struct {
	Port string
}

type DB struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string

	// Пул соединений.
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration

	MigrationsPath string
}

type Config struct {
	HTTP HTTP
	GRPC GRPC
	DB   DB
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded (%v), using environment variables", err)
	}

	return &Config{
		HTTP: HTTP{
			Port: getEnv("STORAGE_GATEWAY_PORT", "8084"),
		},
		GRPC: GRPC{
			Port: getEnv("STORAGE_GATEWAY_GRPC_PORT", "9084"),
		},
		DB: DB{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "click_analytics"),

			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 25),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),
			ConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),

			MigrationsPath: getEnv("STORAGE_GATEWAY_MIGRATIONS_PATH", "file://app/StorageGateway/migrations"),
		},
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("config: invalid int for %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("config: invalid duration for %s=%q, using default %s", key, v, fallback)
		return fallback
	}
	return d
}
