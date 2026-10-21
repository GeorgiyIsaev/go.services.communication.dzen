package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// ClickMessage — формат сообщения в топике clicks.
type ClickMessage struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
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

// discoverPartitions дёргает Metadata API у первого доступного брокера.
func (c *Consumer) discoverPartitions(ctx context.Context) ([]int, error) {
	dialer := &kafka.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Brokers[0])
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.cfg.Brokers[0], err)
	}
	defer conn.Close()

	parts, err := conn.ReadPartitions(c.cfg.Topic)
	if err != nil {
		return nil, fmt.Errorf("read partitions of %q: %w", c.cfg.Topic, err)
	}

	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.ID)
	}
	return ids, nil
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

		at := msg.Time
		if at.IsZero() {
			at = time.Now().UTC()
		}
		c.tracker.AddClickAt(m.AuthorID, m.UserID, at)
		processed++
	}
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
