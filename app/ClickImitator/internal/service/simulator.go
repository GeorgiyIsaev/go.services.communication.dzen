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

var initialSendBackoff = 500 * time.Millisecond

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
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 1
	}
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

	// Отмена контекста — это graceful shutdown, а не ошибка.
	// Проверяем раньше err != nil: если сигнал пришёл, exit code должен быть 0,
	// даже если стратегия успела вернуть агрегированную ошибку.
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

func sendWithRetry(ctx context.Context, sender domain.ClickSender, req domain.ClickEvent, cfg config.Sender, logger *slog.Logger) error {
	attempts := cfg.MaxRetries
	if attempts <= 0 {
		attempts = 1
	}
	timeout := cfg.SendTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	backoff := initialSendBackoff
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
		logger.Warn("send attempt failed",
			"user_id", req.UserID, "author_id", req.AuthorID,
			"attempt", attempt, "max", attempts, "err", err)

		if attempt < attempts {
			half := int64(backoff / 2)
			var jitter time.Duration
			if half > 0 {
				jitter = time.Duration(rand.Int63n(half))
			}
			select {
			case <-time.After(backoff + jitter):
			case <-ctx.Done():
				return nil // graceful abort в паузе
			}
			backoff *= 2
		}
	}

	logger.Error("send exhausted retries",
		"user_id", req.UserID, "author_id", req.AuthorID)
	return fmt.Errorf("send failed after %d attempt(s): %w", attempts, lastErr)
}
