package config

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type GeneratorMode string

const (
	ModeRandom   GeneratorMode = "random"
	ModeScenario GeneratorMode = "scenario"
)

type Kafka struct {
	Brokers []string
	Topic   string
}

type Random struct {
	AuthorIDStart        int64
	AuthorIDEnd          int64
	ReaderIDStart        int64
	ReaderIDEnd          int64
	MinReads             int
	MaxReads             int
	DelayBetweenReadsSec int
}

type Scenario struct {
	Dir                  string
	DelayBetweenReadsSec int
}

// Sender — политика отправки одного клика в Kafka.
type Sender struct {
	RetryDelays []time.Duration
	SendTimeout time.Duration // таймаут одной попытки
}

type Config struct {
	Mode     GeneratorMode
	LogLevel slog.Level
	Kafka    Kafka
	Random   Random
	Scenario Scenario
	Sender   Sender
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	mode := GeneratorMode(getEnv("CLICK_IMITATOR_GENERATOR_MODE", string(ModeRandom)))
	if mode != ModeRandom && mode != ModeScenario {
		log.Printf("config: unknown CLICK_IMITATOR_GENERATOR_MODE=%q, falling back to %q", mode, ModeRandom)
		mode = ModeRandom
	}

	return &Config{
		Mode:     mode,
		LogLevel: parseLogLevel(getEnv("CLICK_IMITATOR_LOG_LEVEL", "info")),
		Kafka: Kafka{
			Brokers: getEnvSlice("KAFKA_BROKERS", []string{
				"localhost:9092", "localhost:9093", "localhost:9094",
			}),
			Topic: getEnv("KAFKA_TOPIC", "clicks"),
		},
		Random: Random{
			AuthorIDStart:        getEnvInt64("CLICK_IMITATOR_AUTHOR_ID_START", 1),
			AuthorIDEnd:          getEnvInt64("CLICK_IMITATOR_AUTHOR_ID_END", 20),
			ReaderIDStart:        getEnvInt64("CLICK_IMITATOR_READER_ID_START", 1),
			ReaderIDEnd:          getEnvInt64("CLICK_IMITATOR_READER_ID_END", 1000),
			MinReads:             getEnvInt("CLICK_IMITATOR_MIN_READS", 10),
			MaxReads:             getEnvInt("CLICK_IMITATOR_MAX_READS", 100),
			DelayBetweenReadsSec: getEnvInt("CLICK_IMITATOR_DELAY_BETWEEN_READS_SEC", 0),
		},
		Scenario: Scenario{
			Dir:                  getEnv("CLICK_IMITATOR_SCENARIO_DIR", "cmd/scenarios"),
			DelayBetweenReadsSec: getEnvInt("CLICK_IMITATOR_DELAY_BETWEEN_READS_SEC", 0),
		},
		Sender: Sender{
			RetryDelays: getEnvDurationSlice("CLICK_IMITATOR_SEND_RETRY_DELAYS",
				[]time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 4 * time.Second}),
			SendTimeout: getEnvDuration("CLICK_IMITATOR_SEND_TIMEOUT", 5*time.Second),
		},
	}
}

func (c *Config) Validate() error {
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("kafka_brokers не заданы")
	}
	if c.Kafka.Topic == "" {
		return fmt.Errorf("kafka_topic не задан")
	}

	switch c.Mode {
	case ModeRandom:
		if c.Random.AuthorIDStart > c.Random.AuthorIDEnd {
			return fmt.Errorf("author_id_start (%d) > author_id_end (%d)",
				c.Random.AuthorIDStart, c.Random.AuthorIDEnd)
		}
		if c.Random.ReaderIDStart > c.Random.ReaderIDEnd {
			return fmt.Errorf("reader_id_start (%d) > reader_id_end (%d)",
				c.Random.ReaderIDStart, c.Random.ReaderIDEnd)
		}
		if c.Random.AuthorIDStart <= 0 {
			return fmt.Errorf("author_id_start должен быть > 0")
		}
		if c.Random.ReaderIDStart <= 0 {
			return fmt.Errorf("reader_id_start должен быть > 0")
		}
		if c.Random.MinReads < 0 {
			return fmt.Errorf("min_reads должен быть >= 0")
		}
		if c.Random.MaxReads < c.Random.MinReads {
			return fmt.Errorf("max_reads (%d) < min_reads (%d)",
				c.Random.MaxReads, c.Random.MinReads)
		}
		if c.Random.DelayBetweenReadsSec < 0 {
			return fmt.Errorf("delay_between_reads_sec должен быть >= 0")
		}
	case ModeScenario:
		if c.Scenario.Dir == "" {
			return fmt.Errorf("scenario_dir не задан")
		}
		if c.Scenario.DelayBetweenReadsSec < 0 {
			return fmt.Errorf("delay_between_reads_sec должен быть >= 0")
		}
	}
	return nil
}

func parseLogLevel(s string) slog.Level {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(s)); err != nil {
		log.Printf("config: invalid CLICK_IMITATOR_LOG_LEVEL=%q, using info", s)
		return slog.LevelInfo
	}
	return lvl
}

// -- helpers --

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

func getEnvInt64(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		log.Printf("config: invalid int64 for %s=%q, using default %d", key, v, fallback)
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

// getEnvDurationSlice парсит список длительностей через запятую: "500ms,1s,2s,4s".
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
