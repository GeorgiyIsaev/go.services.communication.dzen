package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

type Strategy interface {
	Name() string
	TotalUsers() int64
	Run(ctx context.Context, sender domain.ClickSender, cfg config.Sender) error
}

type Simulator struct {
	strategy Strategy
	sender   domain.ClickSender
	cfg      config.Sender
	logger   *slog.Logger
}

func NewSimulator(strategy Strategy, sender domain.ClickSender, cfg config.Sender, logger *slog.Logger) *Simulator {
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = 5 * time.Second
	}
	return &Simulator{strategy: strategy, sender: sender, cfg: cfg, logger: logger}
}

func (s *Simulator) Run(ctx context.Context) error {
	s.logger.Info("simulation started",
		"mode", s.strategy.Name(),
		"users", s.strategy.TotalUsers())

	err := s.strategy.Run(ctx, s.sender, s.cfg)

	// Отмена контекста
	if ctx.Err() != nil {
		s.logger.Info("simulation interrupted")
		return nil
	}
	if err != nil {
		s.logger.Error("simulation stopped with error", "err", err)
		return err
	}
	s.logger.Info("simulation completed")
	return nil
}

// sendWithRetry шлёт клик по расписанию cfg.RetryDelays
func sendWithRetry(ctx context.Context, sender domain.ClickSender, req domain.ClickMessage, cfg config.Sender, logger *slog.Logger) error {
	timeout := cfg.SendTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	attempts := 1 + len(cfg.RetryDelays)

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return nil // graceful abort до попытки
		}

		sendCtx, cancel := context.WithTimeout(ctx, timeout)
		err := sender.Send(sendCtx, req)
		cancel()

		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return nil // graceful abort во время попытки
		}

		lastErr = err
		if attempt == attempts {
			break // расписание исчерпано
		}

		delay := cfg.RetryDelays[attempt-1]
		var jitter time.Duration
		if half := int64(delay / 2); half > 0 {
			jitter = time.Duration(rand.Int63n(half))
		}
		logger.Warn("send attempt failed",
			"user_id", req.UserID, "author_id", req.AuthorID,
			"attempt", attempt, "max", attempts,
			"retry_in", delay+jitter, "err", err)

		select {
		case <-time.After(delay + jitter):
		case <-ctx.Done():
			return nil // graceful abort в паузе
		}
	}

	logger.Error("send exhausted retries",
		"user_id", req.UserID, "author_id", req.AuthorID)
	return fmt.Errorf("send failed after %d attempt(s): %w", attempts, lastErr)
}
