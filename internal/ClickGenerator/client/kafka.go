package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/internal/ClickGenerator/domain"
)

// KafkaClickSender реализует domain.ClickSender через Kafka.
type KafkaClickSender struct {
	writer *kafka.Writer
}

func NewKafkaClickSender(brokers []string, topic string) (*KafkaClickSender, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("не заданы kafka_brokers")
	}
	if topic == "" {
		return nil, fmt.Errorf("не задан kafka_topic")
	}

	w := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		WriteTimeout:           5 * time.Second,
		ReadTimeout:            5 * time.Second,
		AllowAutoTopicCreation: false,
	}

	return &KafkaClickSender{writer: w}, nil
}

func (k *KafkaClickSender) Send(ctx context.Context, req domain.ClickRequest) error {
	value, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга: %w", err)
	}

	// Ключ = UserID. Это позволяет Kafka раскладывать клики одного пользователя в одну партицию.
	key := []byte(strconv.FormatInt(req.AuthorID, 10))

	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}

	if err := k.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("ошибка отправки в Kafka: %w", err)
	}

	return nil
}

func (k *KafkaClickSender) Close() error {
	return k.writer.Close()
}
