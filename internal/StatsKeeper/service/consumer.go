package service

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

// ClickMessage — формат сообщения в топике clicks.
// Должен совпадать с тем, что пишет продюсер.
type ClickMessage struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
}

// Consumer читает клики из Kafka и складывает их в Tracker.
type Consumer struct {
	reader  *kafka.Reader
	tracker *Tracker
}

// NewConsumer создаёт Kafka-консьюмер.
func NewConsumer(brokers []string, topic, groupID string, tracker *Tracker) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6, // 10 MB
		MaxWait:        500 * time.Millisecond,
		CommitInterval: time.Second, // авто-коммит офсетов
		StartOffset:    kafka.FirstOffset,
	})
	return &Consumer{reader: reader, tracker: tracker}
}

// Run блокирующе читает сообщения до отмены ctx.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("kafka read error: %v", err)
			time.Sleep(time.Second)
			continue
		}

		var m ClickMessage
		if err := json.Unmarshal(msg.Value, &m); err != nil {
			log.Printf("invalid message on topic=%s partition=%d offset=%d: %v",
				msg.Topic, msg.Partition, msg.Offset, err)
			continue
		}

		// Используем время из сообщения, а не time.Now():
		// так клики, пришедшие с лагом, попадают в нужный день.
		at := msg.Time
		if at.IsZero() {
			at = time.Now()
		}
		c.tracker.AddClickAt(m.AuthorID, m.UserID, at)
	}
}

// Close закрывает reader.
func (c *Consumer) Close() error {
	return c.reader.Close()
}
