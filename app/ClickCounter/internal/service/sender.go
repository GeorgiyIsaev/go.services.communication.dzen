package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

var (
	ErrFlushExhausted  = errors.New("flush retries exhausted")   //ретраи исчерпаны
	ErrFlushInProgress = errors.New("flush already in progress") //Уже идет flush
)

// StorageGatewayClient — интерфейс отправки в StorageGateway.
type StorageGatewayClient interface {
	SendStats(ctx context.Context, date time.Time, stats []domain.AuthorStat) error
}

// Sender отвечает за отправку данных в StorageGateway.
type Sender struct {
	tracker *Tracker
	client  StorageGatewayClient
	logger  *slog.Logger
	delays  []time.Duration

	// flushMu — «один flush за раз».
	flushMu sync.Mutex

	// mu защищает очередь фоновых отправок.
	mu         sync.Mutex
	pending    time.Time
	hasPending bool
	working    bool // воркер очереди запущен

	rootCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewSender(tracker *Tracker, client StorageGatewayClient, logger *slog.Logger, cfg config.Retries) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	return &Sender{
		tracker: tracker,
		client:  client,
		logger:  logger,
		delays:  cfg.Delays,
		rootCtx: ctx,
		cancel:  cancel,
	}
}

// SendNow ручной flush (POST /flush)
func (s *Sender) SendNow(ctx context.Context, date time.Time) error {
	if !s.flushMu.TryLock() {
		return ErrFlushInProgress
	}
	err := s.flushOnce(ctx, date)
	s.flushMu.Unlock()

	if err == nil {
		return nil
	}
	// Отмена/shutdown — фоновые ретраи не запускаем.
	if ctx.Err() != nil || s.rootCtx.Err() != nil {
		return err
	}
	s.enqueue(date)
	return err
}

// SendAsync ставит дату в фоновую очередь
func (s *Sender) SendAsync(date time.Time) {
	s.enqueue(date)
}

// enqueue кладёт дату в очередь (последняя дата выигрывает) и запускает воркер,
func (s *Sender) enqueue(date time.Time) {
	s.mu.Lock()
	s.pending = date
	s.hasPending = true
	startWorker := !s.working
	if startWorker {
		s.working = true
	}
	s.mu.Unlock()

	if startWorker {
		if s.rootCtx.Err() != nil {
			s.mu.Lock()
			s.working = false
			s.mu.Unlock()
			return
		}
		s.wg.Add(1)
		go s.runPending()
		return
	}
	s.logger.Info("flush in progress, date queued",
		"date", date.Format("2006-01-02"))
}

// runPending фоновый воркер: забирает даты из очереди и шлёт по расписанию.
func (s *Sender) runPending() {
	defer s.wg.Done()

	for {
		s.mu.Lock()
		if !s.hasPending {
			s.working = false
			s.mu.Unlock()
			return
		}
		date := s.pending
		s.hasPending = false
		s.mu.Unlock()

		s.flushMu.Lock()
		err := s.flushWithSchedule(s.rootCtx, date)
		s.flushMu.Unlock()

		switch {
		case err == nil:
			// ok
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			s.logger.Info("flush aborted", "date", date.Format("2006-01-02"), "err", err)
			s.mu.Lock()
			s.working = false
			s.mu.Unlock()
			return
		default:
			// Расписание исчерпано: больше не повторяем, только фиксируем провал.
			s.logger.Error("flush failed, no more retries",
				"date", date.Format("2006-01-02"), "err", err)
		}
	}
}

// flushWithSchedule ретраии по конфигу 1m 2m 4m 8m
func (s *Sender) flushWithSchedule(ctx context.Context, date time.Time) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	err := s.flushOnce(ctx, date)
	if err == nil {
		return nil
	}

	for i, delay := range s.delays {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.logger.Warn("flush: attempt failed, will retry",
			"date", date.Format("2006-01-02"),
			"failed_attempt", i+1,
			"retry_in", delay,
			"err", err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		if err = s.flushOnce(ctx, date); err == nil {
			return nil
		}
	}

	return fmt.Errorf("%w after %d attempt(s): %w", ErrFlushExhausted, len(s.delays)+1, err)
}

// flushOnce конкретная отправка за день
func (s *Sender) flushOnce(ctx context.Context, date time.Time) error {
	day := date.UTC().Truncate(24 * time.Hour)

	stats := s.tracker.GetByDate(day)
	if len(stats) == 0 {
		s.logger.Info("no data to send", "date", day.Format("2006-01-02"))
		return nil
	}

	if err := s.client.SendStats(ctx, day, stats); err != nil {
		return err
	}

	s.logger.Info("stats sent",
		"date", day.Format("2006-01-02"),
		"authors", len(stats))
	return nil
}

// Shutdown отменяет фоновые ретраи и ждёт завершения воркера.
func (s *Sender) Shutdown() {
	s.cancel()
	s.wg.Wait()
}
