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
// Если топик уже существует — проверяет, что число партиций совпадает.
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
	controllerConn, err := dialController(dialer, brokers)
	if err != nil {
		return err
	}
	defer controllerConn.Close()

	// Топик уже есть — проверяем партиции и выходим
	parts, err := controllerConn.ReadPartitions(topic)
	if err == nil && len(parts) > 0 {
		if len(parts) != partitions {
			return fmt.Errorf("топик %q уже существует с %d партициями, ожидалось %d",
				topic, len(parts), partitions)
		}
		return nil
	}

	// Топика нет — создаём
	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: replicationFactor,
	})
	if err != nil {
		if errors.Is(err, kafka.TopicAlreadyExists) {
			return nil // гонка: кто-то создал между ReadPartitions и CreateTopics
		}
		return fmt.Errorf("ошибка создания топика %q: %w", topic, err)
	}

	return nil
}

// dialController перебирает брокеров и возвращает соединение с controller.
// Если ни один брокер недоступен — возвращает агрегированную ошибку.
func dialController(dialer *kafka.Dialer, brokers []string) (*kafka.Conn, error) {
	var lastErr error
	var tried []string

	for _, broker := range brokers {
		conn, err := dialer.Dial("tcp", broker)
		if err != nil {
			lastErr = err
			tried = append(tried, broker)
			continue
		}

		controller, err := conn.Controller()
		if err != nil {
			conn.Close()
			lastErr = err
			tried = append(tried, broker)
			continue
		}

		controllerAddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
		controllerConn, err := dialer.Dial("tcp", controllerAddr)
		conn.Close() // исходное соединение больше не нужно
		if err != nil {
			lastErr = err
			tried = append(tried, broker)
			continue
		}

		return controllerConn, nil
	}

	return nil, fmt.Errorf(
		"не удалось подключиться к controller ни через один брокер (пробовали: %v): %w",
		tried, lastErr,
	)
}
