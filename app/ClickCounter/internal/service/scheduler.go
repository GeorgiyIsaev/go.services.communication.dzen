package service

import (
	"context"
	"log/slog"
	"time"

	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// RunScheduler раз в сутки в каждый из cfg.Hours:cfg.Minute UTC
// запускает асинхронную отправку данных за вчера.
// Возвращается при отмене ctx.
func RunScheduler(ctx context.Context, sender *Sender, logger *slog.Logger, cfg config.Scheduler) {
	logger.Info("scheduler started",
		"hours", cfg.Hours, "minute", cfg.Minute)

	for {
		wait := untilNext(cfg.Hours, cfg.Minute)
		nextAt := time.Now().UTC().Add(wait)

		logger.Info("scheduler: next run scheduled",
			"in", wait, "at", nextAt.Format(time.RFC3339))

		select {
		case <-ctx.Done():
			logger.Info("scheduler stopped")
			return
		case <-time.After(wait):
			// Отправляем данные за вчера (в UTC).
			date := time.Now().UTC().AddDate(0, 0, -1)
			logger.Info("scheduler: firing", "date", date.Format("2006-01-02"))
			sender.SendAsync(date)
		}
	}
}

// untilNext возвращает минимальное время до одного из cfg.Hours:cfg.Minute UTC.
func untilNext(hours []int, minute int) time.Duration {
	return untilNextFrom(time.Now().UTC(), hours, minute)
}

// untilNextFrom — чистая функция, тестируемая.
func untilNextFrom(now time.Time, hours []int, minute int) time.Duration {
	var best time.Duration
	for _, h := range hours {
		candidate := time.Date(now.Year(), now.Month(), now.Day(), h, minute, 0, 0, time.UTC)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		if d := candidate.Sub(now); best == 0 || d < best {
			best = d
		}
	}
	return best
}
