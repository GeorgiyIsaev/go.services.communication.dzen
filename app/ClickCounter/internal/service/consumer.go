package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// ClickMessage — формат сообщения в топике clicks.
// Контракт зеркалит domain.ClickEvent имитатора: дату клика определяет
// имитатор и пишет её в timestamp. Поле обязательно: нулевой timestamp =
// нарушение контракта, сообщение отбрасывается (без fallback'ов).
type ClickMessage struct {
	UserID    int64     `json:"user_id"`
	AuthorID  int64     `json:"author_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Consumer читает клики из всех партиций топика
type Consumer struct {
	cfg     config.Kafka
	tracker *Tracker
	logger  *slog.Logger

	mu      sync.Mutex
	readers []*kafka.Reader
}

func NewConsumer(cfg config.Kafka, tracker *Tracker, logger *slog.Logger) *Consumer {
	return &Consumer{
		cfg:     cfg,
		tracker: tracker,
		logger:  logger,
	}
}

// Run блокируется, пока ctx не отменён. На каждую партицию — свой reader.
func (c *Consumer) Run(ctx context.Context) error {
	partitions, err := c.discoverPartitions(ctx)
	if err != nil {
		return fmt.Errorf("discover partitions: %w", err)
	}
	c.logger.Info("kafka: partitions discovered",
		"topic", c.cfg.Topic, "count", len(partitions), "partitions", partitions)

	var wg sync.WaitGroup
	for _, p := range partitions {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     c.cfg.Brokers,
			Topic:       c.cfg.Topic,
			Partition:   p,
			StartOffset: kafka.FirstOffset,
			MinBytes:    1,
			MaxBytes:    10e6,
			MaxWait:     500 * time.Millisecond,
		})

		c.mu.Lock()
		c.readers = append(c.readers, r)
		c.mu.Unlock()

		wg.Add(1)
		go func(partition int, r *kafka.Reader) {
			defer wg.Done()
			c.consumePartition(ctx, partition, r)
		}(p, r)
	}

	wg.Wait()
	c.logger.Info("kafka: all partition readers stopped")
	return nil
}

// discoverPartitions запрашивает метаданные у брокеров по очереди
func (c *Consumer) discoverPartitions(ctx context.Context) ([]int, error) {
	return discoverPartitionsFrom(ctx, c.cfg.Brokers, func(ctx context.Context, broker string) ([]kafka.Partition, error) {
		dialer := &kafka.Dialer{Timeout: 10 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			c.logger.Warn("kafka: broker unavailable",
				"broker", broker, "err", err)
			return nil, err
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		parts, err := conn.ReadPartitions(c.cfg.Topic)
		if err != nil {
			c.logger.Warn("kafka: broker unavailable",
				"broker", broker, "err", err)
			return nil, err
		}
		return parts, nil
	})
}

// discoverPartitionsFrom идёт по списку брокеров до успеха.
func discoverPartitionsFrom(
	ctx context.Context,
	brokers []string,
	fn func(ctx context.Context, broker string) ([]kafka.Partition, error),
) ([]int, error) {
	if len(brokers) == 0 {
		return nil, errors.New("no kafka brokers configured")
	}

	var errs []error
	for _, broker := range brokers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		parts, err := fn(ctx, broker)
		if err != nil {
			errs = append(errs, fmt.Errorf("broker %s: %w", broker, err))
			continue
		}

		ids := make([]int, 0, len(parts))
		seen := make(map[int]struct{}, len(parts))
		for _, p := range parts {
			if _, ok := seen[p.ID]; ok {
				continue
			}
			seen[p.ID] = struct{}{}
			ids = append(ids, p.ID)
		}
		return ids, nil
	}

	return nil, fmt.Errorf("kafka: no broker available (%d tried): %w", len(brokers), errors.Join(errs...))
}

func (c *Consumer) consumePartition(ctx context.Context, partition int, r *kafka.Reader) {
	log := c.logger.With("partition", partition)
	log.Info("kafka: partition reader started")

	var processed uint64
	defer func() {
		log.Info("kafka: partition reader stopped", "messages_processed", processed)
	}()

	for {
		msg, err := r.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("kafka read error", "err", err)
			time.Sleep(time.Second)
			continue
		}

		var m ClickMessage
		if err := json.Unmarshal(msg.Value, &m); err != nil {
			log.Warn("invalid message", "offset", msg.Offset, "err", err)
			continue
		}
		if m.UserID <= 0 || m.AuthorID <= 0 {
			log.Warn("invalid ids", "offset", msg.Offset,
				"user_id", m.UserID, "author_id", m.AuthorID)
			continue
		}

		at, ok := eventTime(m)
		if !ok {
			log.Warn("contract violation: message without timestamp, dropped",
				"offset", msg.Offset,
				"user_id", m.UserID, "author_id", m.AuthorID)
			continue
		}
		c.tracker.AddClickAt(m.AuthorID, m.UserID, at)
		processed++
	}
}

// eventTime возвращает момент клика из контрактного поля timestamp —
// его определяет имитатор (см. domain.ClickEvent).
// Нулевое поле = нарушение контракта: (zero, false), сообщение отбрасывается
// вызывающим. Fallback'ов на время брокера или «сейчас» нет — политика
// «только контракт, нарушителей отбрасываем».
func eventTime(m ClickMessage) (time.Time, bool) {
	if m.Timestamp.IsZero() {
		return time.Time{}, false
	}
	return m.Timestamp, true
}

// Close закрывает все ридеры. Вызывать после возврата из Run.
func (c *Consumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for _, r := range c.readers {
		if err := r.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
