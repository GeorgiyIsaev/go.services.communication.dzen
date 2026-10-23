package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
)

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

func (k *KafkaClickSender) Send(ctx context.Context, req domain.ClickEvent) error {
	value, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга: %w", err)
	}
	// Ключ = AuthorID. Клики одного автора попадают в одну партицию.
	key := []byte(strconv.FormatInt(req.AuthorID, 10))
	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now().UTC(),
	}
	if err := k.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("ошибка отправки в Kafka: %w", err)
	}
	return nil
}

func (k *KafkaClickSender) Close() error {
	return k.writer.Close()
}

// VerifyTopicExists проверяет, топик существует?, логирует, если нет ошибка.
func VerifyTopicExists(ctx context.Context, brokers []string, topic string, logger *slog.Logger) error {
	if topic == "" {
		return errors.New("topic не задан")
	}

	var lastErr error
	for _, broker := range brokers {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := kafka.DialContext(dialCtx, "tcp", broker)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("broker %s: %w", broker, err)
			continue
		}

		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		partitions, err := conn.ReadPartitions(topic)
		_ = conn.Close()
		if err != nil {
			lastErr = fmt.Errorf("broker %s: %w", broker, err)
			continue
		}
		if len(partitions) == 0 {
			lastErr = fmt.Errorf("broker %s: топик %q пустой (0 партиций)", broker, topic)
			continue
		}

		//ID лидеров партиций
		leaders := make(map[int]struct{}, len(partitions))
		for _, p := range partitions {
			leaders[p.Leader.ID] = struct{}{}
		}

		logger.Info("kafka topic verified",
			"topic", topic,
			"broker", broker,
			"partitions", len(partitions),
			"leaders", len(leaders),
		)
		return nil
	}

	return fmt.Errorf("не удалось проверить топик %q ни на одном брокере: %w", topic, lastErr)
}
