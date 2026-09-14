package domain

import (
	"context"
	"fmt"
)

type ClickRequest struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
}

type Config struct {
	KafkaBrokers           []string `json:"kafka_brokers"`
	KafkaTopic             string   `json:"kafka_topic"`
	KafkaPartitions        int      `json:"kafka_partitions"`
	KafkaReplicationFactor int      `json:"kafka_replication_factor"`
	ScenarioDir            string   `json:"scenario_dir"`
	DelayBetweenReadsSec   int      `json:"delay_between_reads_sec"`
	MaxRetries             int      `json:"max_retries"`
}

// ClickSender контракт на отправку клика (порт)
type ClickSender interface {
	Send(ctx context.Context, req ClickRequest) error
}

// Scenario описывает сценарий чтений одного пользователя.
type Scenario struct {
	UserID  int64
	Authors []int64
}

// Validate проверяет, что сценарий заполнен корректно.
func (s Scenario) Validate() error {
	if s.UserID == 0 {
		return fmt.Errorf("не задан user_id")
	}
	if len(s.Authors) == 0 {
		return fmt.Errorf("для user_id=%d не задан список authors", s.UserID)
	}
	return nil
}
