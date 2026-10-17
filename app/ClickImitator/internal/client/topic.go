package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

const (
	topicDialTimeout = 5 * time.Second
	topicRetryStart  = 500 * time.Millisecond
	topicRetryMax    = 5 * time.Second
	topicRetryLimit  = 15
)

// EnsureTopic создаёт топик, если его нет, либо проверяет параметры
// существующего. Свежезапущенная Kafka часто рвёт Metadata-запросы
// (EOF) во время лидер-элекшена, поэтому вся операция повторяется
// с экспоненциальным backoff.
func EnsureTopic(ctx context.Context, cfg config.Kafka, logger *slog.Logger) error {
	if len(cfg.Brokers) == 0 {
		return errors.New("kafka_brokers не заданы")
	}
	if cfg.Topic == "" {
		return errors.New("kafka_topic не задан")
	}
	if cfg.Partitions <= 0 || cfg.ReplicationFactor <= 0 {
		return fmt.Errorf("некорректные partitions=%d rf=%d", cfg.Partitions, cfg.ReplicationFactor)
	}

	backoff := topicRetryStart
	var lastErr error
	for attempt := 1; attempt <= topicRetryLimit; attempt++ {
		err := ensureTopicOnce(ctx, cfg, logger)
		if err == nil {
			return nil
		}
		lastErr = err
		logger.Warn("ensure topic attempt failed",
			"attempt", attempt, "max", topicRetryLimit, "err", err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		if backoff < topicRetryMax {
			backoff *= 2
		}
	}
	return fmt.Errorf("ensure topic failed after %d attempts: %w", topicRetryLimit, lastErr)
}

func ensureTopicOnce(ctx context.Context, cfg config.Kafka, logger *slog.Logger) error {
	conn, err := dialAny(ctx, cfg.Brokers)
	if err != nil {
		return err
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("controller: %w", err)
	}

	ctrl, err := kafka.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("dial controller: %w", err)
	}
	defer ctrl.Close()

	parts, err := ctrl.ReadPartitions(cfg.Topic)
	switch {
	case err == nil && len(parts) > 0:
		return verifyTopic(cfg, parts, logger)
	case err != nil && !errors.Is(err, kafka.UnknownTopicOrPartition):
		return fmt.Errorf("read partitions: %w", err)
	}

	return createTopic(ctrl, cfg, logger)
}

func dialAny(ctx context.Context, brokers []string) (*kafka.Conn, error) {
	d := &kafka.Dialer{Timeout: topicDialTimeout, DualStack: true}
	var lastErr error
	for _, addr := range brokers {
		c, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("нет доступных брокеров: %w", lastErr)
}

func createTopic(ctrl *kafka.Conn, cfg config.Kafka, logger *slog.Logger) error {
	err := ctrl.CreateTopics(kafka.TopicConfig{
		Topic:             cfg.Topic,
		NumPartitions:     cfg.Partitions,
		ReplicationFactor: cfg.ReplicationFactor,
		ConfigEntries: []kafka.ConfigEntry{
			{ConfigName: "retention.ms", ConfigValue: fmt.Sprintf("%d", cfg.Retention.Milliseconds())},
			{ConfigName: "segment.ms", ConfigValue: fmt.Sprintf("%d", cfg.Segment.Milliseconds())},
		},
	})
	if err == nil {
		logger.Info("topic created",
			"topic", cfg.Topic, "partitions", cfg.Partitions, "rf", cfg.ReplicationFactor)
		return nil
	}

	// Топик мог быть создан параллельно — перечитываем.
	if parts, rerr := ctrl.ReadPartitions(cfg.Topic); rerr == nil && len(parts) > 0 {
		return verifyTopic(cfg, parts, logger)
	}
	return fmt.Errorf("create topic %q: %w", cfg.Topic, err)
}

func verifyTopic(cfg config.Kafka, parts []kafka.Partition, logger *slog.Logger) error {
	if len(parts) < cfg.Partitions {
		return fmt.Errorf("топик %q: partitions=%d < требуемых %d",
			cfg.Topic, len(parts), cfg.Partitions)
	}
	if rf := len(parts[0].Replicas); rf < cfg.ReplicationFactor {
		return fmt.Errorf("топик %q: rf=%d < требуемых %d",
			cfg.Topic, rf, cfg.ReplicationFactor)
	}
	logger.Info("topic exists",
		"topic", cfg.Topic, "partitions", len(parts), "rf", len(parts[0].Replicas))
	return nil
}
