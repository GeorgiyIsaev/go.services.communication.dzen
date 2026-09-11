package client

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"
)

// EnsureTopic создаёт топик с заданным числом партиций и replication factor.
// Если топик уже существует — возвращает nil (не считаем это ошибкой).
func EnsureTopic(brokers []string, topic string, partitions, replicationFactor int) error {
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

	dialer := &kafka.Dialer{
		Timeout:   10 * time.Second,
		DualStack: true,
	}

	// Подключаемся к произвольному брокеру, чтобы узнать controller
	conn, err := dialer.Dial("tcp", brokers[0])
	if err != nil {
		return fmt.Errorf("не удалось подключиться к брокеру %s: %w", brokers[0], err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("не удалось получить controller: %w", err)
	}

	controllerAddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	controllerConn, err := dialer.Dial("tcp", controllerAddr)
	if err != nil {
		return fmt.Errorf("не удалось подключиться к controller %s: %w", controllerAddr, err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		// Kafka возвращает "Topic with this name already exists" — игнорируем
		if errors.Is(err, kafka.TopicAlreadyExists) {
			return nil
		}
		return fmt.Errorf("ошибка создания топика %q: %w", topic, err)
	}

	return nil
}
