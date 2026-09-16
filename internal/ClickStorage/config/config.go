package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost          string
	DBPort          string
	DBUser          string
	DBPassword      string
	DBName          string
	StatsURL        string
	ServerPort      string
	StatsBatchSize  int
	StatsMaxRetries int
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	return &Config{
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          getEnv("DB_PORT", "5432"),
		DBUser:          getEnv("DB_USER", "postgres"),
		DBPassword:      getEnv("DB_PASSWORD", ""),
		DBName:          getEnv("DB_NAME", "click_analytics"),
		StatsURL:        getEnv("STATS_SERVICE_URL", "http://external-service/stats"),
		ServerPort:      getEnv("PORT_CLICK_STORAGE", "8084"), // по умолчанию 8084
		StatsBatchSize:  getEnvInt("STATS_BATCH_SIZE", 100),
		StatsMaxRetries: getEnvInt("STATS_MAX_RETRIES", 3),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}
