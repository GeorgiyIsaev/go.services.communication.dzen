package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

var ErrFlushBudgetExhausted = errors.New("flush budget exhausted")

// StorageGatewayClient — интерфейс, объявлен у потребителя (service).
type StorageGatewayClient interface {
	SendStats(ctx context.Context, date time.Time, stats []domain.AuthorStat) error
}

// Sender отвечает за отправку вчерашних данных в StorageGateway.
// Единственный триггер — scheduler. Параллельные flush'и подавляются.
type Sender struct {
	tracker *Tracker
	client  StorageGatewayClient
	logger  *slog.Logger

	maxRetries  int
	retryDelay  time.Duration
	longDelay   time.Duration
	maxDuration time.Duration

	sending atomic.Bool

	rootCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewSender(tracker *Tracker, client StorageGatewayClient, logger *slog.Logger, cfg config.Retries) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	return &Sender{
		tracker:     tracker,
		client:      client,
		logger:      logger,
		maxRetries:  cfg.MaxRetries,
		retryDelay:  cfg.Delay,
		longDelay:   cfg.LongDelay,
		maxDuration: cfg.MaxDuration,
		rootCtx:     ctx,
		cancel:      cancel,
	}
}

// SendAsync запускает отправку в фоне и сразу возвращается
func (s *Sender) SendAsync(date time.Time) {
	//(защита от параллельных запусков).
	if !s.sending.CompareAndSwap(false, true) {
		s.logger.Info("send already in progress, skipping",
			"date", date.Format("2006-01-02"))
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.sending.Store(false)

		if err := s.flush(s.rootCtx, date); err != nil {
			s.logger.Error("flush failed",
				"date", date.Format("2006-01-02"), "err", err)
		}
	}()
}

// SendNow — синхронная отправка. Используется ручкой /flush
func (s *Sender) SendNow(ctx context.Context, date time.Time) error {
	if !s.sending.CompareAndSwap(false, true) {
		return errors.New("send already in progress")
	}
	defer s.sending.Store(false)
	return s.flush(ctx, date)
}

// flush отправляет данные за указанную дату с ретраями
func (s *Sender) flush(ctx context.Context, date time.Time) error {
	date = date.UTC()
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	deadline := time.Now().Add(s.maxDuration)

	attempt := 0
	for {
		if time.Now().After(deadline) {
			return ErrFlushBudgetExhausted
		}
		attempt++

		stats := s.tracker.GetByDate(day)
		if len(stats) == 0 {
			s.logger.Info("no data to send", "date", day.Format("2006-01-02"))
			return nil
		}

		err := s.client.SendStats(ctx, day, stats)
		if err == nil {
			s.logger.Info("stats sent",
				"date", day.Format("2006-01-02"),
				"authors", len(stats),
				"attempts", attempt)
			return nil
		}

		s.logger.Warn("send attempt failed",
			"date", day.Format("2006-01-02"),
			"attempt", attempt, "err", err)

		wait := s.retryDelay
		if s.maxRetries > 0 && attempt%s.maxRetries == 0 {
			wait = s.longDelay
			s.logger.Warn("retries exhausted, long pause",
				"attempt", attempt, "wait", wait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// Shutdown отменяет фоновые отправки и ждёт их завершения
func (s *Sender) Shutdown() {
	s.cancel()
	s.wg.Wait()
}
