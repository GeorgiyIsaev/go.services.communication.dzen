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

	// 1. Топик уже есть? Тогда только сверяем партиции.
	if err := checkExistingTopic(controllerConn, topic, partitions); err == nil {
		return nil
	} else if !errors.Is(err, errTopicNotFound) {
		return err
	}

	// 2. Топика нет — создаём.
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: replicationFactor,
		},
	}

	if err := controllerConn.CreateTopics(topicConfigs...); err != nil {
		if !isTopicAlreadyExists(err) {
			return fmt.Errorf("ошибка создания топика %q: %w", topic, err)
		}
		// Гонка: кто-то создал топик между нашими чтением и созданием.
		// Сверяем партиции ещё раз, чтобы не пропустить конфликт конфигураций.
		if err := checkExistingTopic(controllerConn, topic, partitions); err != nil {
			return err
		}
	}

	return nil
}

// errTopicNotFound — внутренний маркер «топик ещё не создан».
var errTopicNotFound = errors.New("топик не найден")

// checkExistingTopic читает метаданные топика и сверяет число партиций.
// Возвращает errTopicNotFound, если топика нет.
func checkExistingTopic(conn *kafka.Conn, topic string, want int) error {
	parts, err := conn.ReadPartitions(topic)
	if err != nil {
		// kafka-go возвращает ошибку, если топик не существует.
		return errTopicNotFound
	}
	if len(parts) == 0 {
		return errTopicNotFound
	}
	if len(parts) != want {
		return fmt.Errorf("топик %q уже существует с %d партициями, ожидалось %d",
			topic, len(parts), want)
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
