package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// HTTP — параметры HTTP-сервера. Используется в main.
type HTTP struct {
	Port              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

// Kafka — параметры подключения к топику кликов.
// GroupID намеренно отсутствует: читаем без группы,
// чтобы StartOffset=FirstOffset применялся при каждом старте.
type Kafka struct {
	Brokers []string
	Topic   string
}

// Gateway — параметры HTTP-клиента к StorageGateway.
type Gateway struct {
	URL     string
	Timeout time.Duration
}

// Retries — политика ретраев flush в StorageGateway. Используется в service.Sender.
// Delays — расписание пауз между попытками (по возрастанию): после неудачной
// попытки ждём Delays[0], затем Delays[1] и т.д.; исчерпание расписания = провал.
// Пустой Delays = ретраев нет (только одна попытка).
type Retries struct {
	Delays []time.Duration
}

// Scheduler — моменты ежедневной отправки. Hours: [0, 1] → 00:05 и 01:05 UTC.
type Scheduler struct {
	Hours  []int
	Minute int
}

type Config struct {
	HTTP      HTTP
	Kafka     Kafka
	Gateway   Gateway
	Retries   Retries
	Scheduler Scheduler
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	return &Config{
		HTTP: HTTP{
			Port:              getEnv("PORT_CLICK_COUNTER", "8080"),
			ReadHeaderTimeout: getEnvDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getEnvDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      getEnvDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		},
		Kafka: Kafka{
			Brokers: getEnvSlice("KAFKA_BROKERS", []string{
				"localhost:9092", "localhost:9093", "localhost:9094",
			}),
			Topic: getEnv("KAFKA_TOPIC", "clicks"),
		},
		Gateway: Gateway{
			URL:     getEnv("STORAGE_GATEWAY_URL", "http://localhost:8084"),
			Timeout: getEnvDuration("STORAGE_GATEWAY_TIMEOUT", 10*time.Second),
		},
		Retries: Retries{
			// Дефолт: экспоненциальные паузы 1+2+4+8 = 15 минут, 5 попыток.
			// После исчерпания — фиксируем провал в логе и больше не повторяем.
			Delays: getEnvDurationSlice("FLUSH_RETRY_DELAYS",
				[]time.Duration{1 * time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}),
		},
		Scheduler: Scheduler{
			Hours:  getEnvIntSlice("SCHEDULER_HOURS", []int{0, 1}),
			Minute: getEnvInt("SCHEDULER_MINUTE", 5),
		},
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvSlice(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func getEnvIntSlice(key string, fallback []int) []int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			log.Printf("config: invalid int in %s=%q, using default %v", key, v, fallback)
			return fallback
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
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

// getEnvDurationSlice парсит список длительностей через запятую: "1m,2m,4m,8m".
// Некорректный элемент или пустое значение → fallback целиком.
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
