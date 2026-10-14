package domain

import (
	"context"
	"fmt"
	"time"
)

type ClickEvent struct {
	UserID    int64     `json:"user_id"`
	AuthorID  int64     `json:"author_id"`
	Timestamp time.Time `json:"timestamp"`
}

type Config struct {
	KafkaBrokers           []string `json:"kafka_brokers"`
	KafkaTopic             string   `json:"kafka_topic"`
	KafkaPartitions        int      `json:"kafka_partitions"`
	KafkaReplicationFactor int      `json:"kafka_replication_factor"`
	AuthorIDStart          int64    `json:"author_id_start"`
	AuthorIDEnd            int64    `json:"author_id_end"`
	ReaderIDStart          int64    `json:"reader_id_start"`
	ReaderIDEnd            int64    `json:"reader_id_end"`
	MinReads               int      `json:"min_reads"`
	MaxReads               int      `json:"max_reads"`
	DelayBetweenReadsSec   int      `json:"delay_between_reads_sec"`
	MaxRetries             int      `json:"max_retries"`
}

// ClickSender контракт на отправку клика (порт)
type ClickSender interface {
	Send(ctx context.Context, req ClickEvent) error
}

// Валидация конфигурай
func (c Config) Validate() error {
	if len(c.KafkaBrokers) == 0 {
		return fmt.Errorf("kafka_brokers не заданы")
	}
	if c.KafkaTopic == "" {
		return fmt.Errorf("kafka_topic не задан")
	}
	if c.KafkaPartitions <= 0 {
		return fmt.Errorf("kafka_partitions должен быть > 0")
	}
	if c.KafkaReplicationFactor <= 0 {
		return fmt.Errorf("kafka_replication_factor должен быть > 0")
	}
	if c.AuthorIDStart > c.AuthorIDEnd {
		return fmt.Errorf("author_id_start (%d) > author_id_end (%d)",
			c.AuthorIDStart, c.AuthorIDEnd)
	}
	if c.ReaderIDStart > c.ReaderIDEnd {
		return fmt.Errorf("reader_id_start (%d) > reader_id_end (%d)",
			c.ReaderIDStart, c.ReaderIDEnd)
	}
	if c.MinReads < 0 {
		return fmt.Errorf("min_reads должен быть >= 0")
	}
	if c.MaxReads < c.MinReads {
		return fmt.Errorf("max_reads (%d) < min_reads (%d)",
			c.MaxReads, c.MinReads)
	}
	if c.DelayBetweenReadsSec < 0 {
		return fmt.Errorf("delay_between_reads_sec должен быть >= 0")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries должен быть >= 0")
	}
	return nil
}
