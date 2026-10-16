package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
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

// EnsureTopic создаёт топик, если его нет, с нужным числом партиций
// и retention. Если топик уже существует — проверяет только существование
// (Kafka не даёт изменить число партиций меньшее текущего; retention можно
// поменять через AlterConfigs, но это отдельная операция).
//
// Параметры retention и segment передаются как time.Duration
// и конвертируются в миллисекунды (Kafka оперирует мс).
func EnsureTopic(brokers []string, topic string, partitions, replicationFactor int, retention, segment time.Duration) error {
	if len(brokers) == 0 {
		return fmt.Errorf("не заданы kafka_brokers")
	}
	if topic == "" {
		return fmt.Errorf("не задан kafka_topic")
	}
	if partitions <= 0 {
		return fmt.Errorf("kafka_partitions должен быть > 0")
	}
	if replicationFactor <= 0 {
		return fmt.Errorf("kafka_replication_factor должен быть > 0")
	}

	conn, err := kafka.Dial("tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("не удалось подключиться к Kafka: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("не удалось получить controller: %w", err)
	}
	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("не удалось подключиться к controller: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
			ConfigEntries: []kafka.ConfigEntry{
				{ConfigName: "retention.ms", ConfigValue: strconv.FormatInt(retention.Milliseconds(), 10)},
				{ConfigName: "segment.ms", ConfigValue: strconv.FormatInt(segment.Milliseconds(), 10)},
			},
		},
	}

	// CreateTopics идемпотентен: если топик уже есть с такими же
	// параметрами — ошибки не будет. Если параметры отличаются —
	// вернётся ошибка, но топик останется как есть.
	if err := controllerConn.CreateTopics(topicConfigs...); err != nil {
		return fmt.Errorf("не удалось создать/проверить топик %q: %w", topic, err)
	}
	return nil
}
