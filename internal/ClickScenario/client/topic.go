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

	conn, err := dialAnyBroker(dialer, brokers)
	if err != nil {
		return err
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

	if err := controllerConn.CreateTopics(topicConfigs...); err != nil {
		if isTopicAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("ошибка создания топика %q: %w", topic, err)
	}
	return nil
}

func dialAnyBroker(dialer *kafka.Dialer, brokers []string) (*kafka.Conn, error) {
	var conn *kafka.Conn
	var lastErr error

	for _, addr := range brokers {
		conn, lastErr = dialer.Dial("tcp", addr)
		if lastErr == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("не удалось подключиться ни к одному брокеру из %v: %w", brokers, lastErr)
}

func isTopicAlreadyExists(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, kafka.TopicAlreadyExists) {
		return true
	}

	var kerr kafka.Error
	if errors.As(err, &kerr) && kerr == kafka.TopicAlreadyExists {
		return true
	}

	return false
}
