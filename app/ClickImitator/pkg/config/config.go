package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// GeneratorMode — режим работы имитатора.
type GeneratorMode string

const (
	ModeRandom   GeneratorMode = "random"
	ModeScenario GeneratorMode = "scenario"
)

// Kafka — параметры подключения и топика.
type Kafka struct {
	Brokers           []string
	Topic             string
	Partitions        int
	ReplicationFactor int
	Retention         time.Duration // TTL сообщений в топике
	Segment           time.Duration // период ротации сегментов
}

// Random — параметры случайной генерации (режим random).
type Random struct {
	AuthorIDStart        int64
	AuthorIDEnd          int64
	ReaderIDStart        int64
	ReaderIDEnd          int64
	MinReads             int
	MaxReads             int
	DelayBetweenReadsSec int
}

// Scenario — параметры сценарного режима (режим scenario).
type Scenario struct {
	Dir                  string
	DelayBetweenReadsSec int
}

// Sender — общие параметры отправки в Kafka.
type Sender struct {
	MaxRetries int
}

type Config struct {
	Mode     GeneratorMode
	Kafka    Kafka
	Random   Random
	Scenario Scenario
	Sender   Sender
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	mode := GeneratorMode(getEnv("GENERATOR_MODE", string(ModeRandom)))
	if mode != ModeRandom && mode != ModeScenario {
		log.Printf("config: unknown GENERATOR_MODE=%q, falling back to %q", mode, ModeRandom)
		mode = ModeRandom
	}

	return &Config{
		Mode: mode,
		Kafka: Kafka{
			Brokers:           getEnvSlice("KAFKA_BROKERS", []string{"localhost:9092", "localhost:9093", "localhost:9094"}),
			Topic:             getEnv("KAFKA_TOPIC", "clicks"),
			Partitions:        getEnvInt("KAFKA_PARTITIONS", 8),
			ReplicationFactor: getEnvInt("KAFKA_REPLICATION_FACTOR", 3),
			Retention:         getEnvDuration("KAFKA_RETENTION", 48*time.Hour),
			Segment:           getEnvDuration("KAFKA_SEGMENT", 24*time.Hour),
		},
		Random: Random{
			AuthorIDStart:        getEnvInt64("AUTHOR_ID_START", 1),
			AuthorIDEnd:          getEnvInt64("AUTHOR_ID_END", 20),
			ReaderIDStart:        getEnvInt64("READER_ID_START", 1),
			ReaderIDEnd:          getEnvInt64("READER_ID_END", 1000),
			MinReads:             getEnvInt("MIN_READS", 10),
			MaxReads:             getEnvInt("MAX_READS", 100),
			DelayBetweenReadsSec: getEnvInt("DELAY_BETWEEN_READS_SEC", 0),
		},
		Scenario: Scenario{
			Dir:                  getEnv("SCENARIO_DIR", "cmd/scenarios"),
			DelayBetweenReadsSec: getEnvInt("DELAY_BETWEEN_READS_SEC", 0),
		},
		Sender: Sender{
			MaxRetries: getEnvInt("MAX_RETRIES", 5),
		},
	}
}

// Validate проверяет конфиг в зависимости от выбранного режима.
func (c *Config) Validate() error {
	if len(c.Kafka.Brokers) == 0 {
		return fmt.Errorf("kafka_brokers не заданы")
	}
	if c.Kafka.Topic == "" {
		return fmt.Errorf("kafka_topic не задан")
	}
	if c.Kafka.Partitions <= 0 {
		return fmt.Errorf("kafka_partitions должен быть > 0")
	}
	if c.Kafka.ReplicationFactor <= 0 {
		return fmt.Errorf("kafka_replication_factor должен быть > 0")
	}
	if c.Sender.MaxRetries < 0 {
		return fmt.Errorf("max_retries должен быть >= 0")
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
