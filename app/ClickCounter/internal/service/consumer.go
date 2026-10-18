package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// ClickMessage — формат сообщения в топике clicks.
type ClickMessage struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
}

// Consumer читает клики из Kafka и складывает их в Tracker.
type Consumer struct {
	reader  *kafka.Reader
	tracker *Tracker
	logger  *slog.Logger
}

func NewConsumer(cfg config.Kafka, tracker *Tracker, logger *slog.Logger) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   cfg.Topic,
		// GroupID НЕ выставляем — при каждом старте читаем сначала
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
		MaxWait:     500 * time.Millisecond,
	})
	return &Consumer{reader: reader, tracker: tracker, logger: logger}
}

func (c *Consumer) Run(ctx context.Context) error {
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.Error("kafka read error", "err", err)
			time.Sleep(time.Second)
			continue
		}

		var m ClickMessage
		if err := json.Unmarshal(msg.Value, &m); err != nil {
			c.logger.Warn("invalid message",
				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "err", err)
			continue
		}
		if m.UserID <= 0 || m.AuthorID <= 0 {
			c.logger.Warn("invalid ids",
				"topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset,
				"user_id", m.UserID, "author_id", m.AuthorID)
			continue
		}

		at := msg.Time
		if at.IsZero() {
			at = time.Now().UTC() // ← UTC
		}
		c.tracker.AddClickAt(m.AuthorID, m.UserID, at)
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
