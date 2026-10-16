package service

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
)

// initialSendBackoff — стартовая пауза перед повтором. (для тестов)
var initialSendBackoff = 500 * time.Millisecond

// Strategy — источник «что и кому отправить». Random и Scenario реализуют его.
type Strategy interface {
	// Name — для логов.
	Name() string
	// TotalUsers — сколько пользователей в сценарии (для прогресс-логов).
	TotalUsers() int64
	// Run запускает симуляцию до завершения или отмены ctx.
	Run(ctx context.Context, sender domain.ClickSender, maxRetries int) error
}

// Simulator — общий каркас, который держит стратегию и логирует прогресс.
type Simulator struct {
	strategy   Strategy
	sender     domain.ClickSender
	maxRetries int
	logger     *slog.Logger

	completed int64 // atomic
}

func NewSimulator(strategy Strategy, sender domain.ClickSender, maxRetries int, logger *slog.Logger) *Simulator {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	return &Simulator{
		strategy:   strategy,
		sender:     sender,
		maxRetries: maxRetries,
		logger:     logger,
	}
}

func (s *Simulator) Run(ctx context.Context) {
	s.logger.Info("simulation started",
		"mode", s.strategy.Name(),
		"users", s.strategy.TotalUsers())

	if err := s.strategy.Run(ctx, s.sender, s.maxRetries); err != nil {
		s.logger.Error("simulation stopped with error", "err", err)
		return
	}

	if ctx.Err() != nil {
		s.logger.Info("simulation interrupted")
		return
	}
	s.logger.Info("simulation completed")
}

// sendWithRetry — общая функция отправки с ретраями и экспоненциальным backoff.
func sendWithRetry(ctx context.Context, sender domain.ClickSender, req domain.ClickEvent, maxRetries int, logger *slog.Logger) {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	backoff := initialSendBackoff

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			logger.Info("send interrupted",
				"user_id", req.UserID, "author_id", req.AuthorID, "err", err)
			return
		}

		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := sender.Send(sendCtx, req)
		cancel()

		if err == nil {
			return
		}

		logger.Warn("send attempt failed",
			"user_id", req.UserID, "author_id", req.AuthorID,
			"attempt", attempt, "max", maxRetries, "err", err)

		if attempt < maxRetries {
			half := int64(backoff / 2)
			var jitter time.Duration
			if half > 0 {
				jitter = time.Duration(rand.Int63n(half))
			}
			select {
			case <-time.After(backoff + jitter):
			case <-ctx.Done():
				return
			}
			backoff *= 2
		}
	}

	logger.Error("send exhausted retries",
		"user_id", req.UserID, "author_id", req.AuthorID)
}
