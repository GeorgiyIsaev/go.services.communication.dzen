package scheduler

import (
	"context"
	"log"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/timeutil"
)

// Run запускает фоновый процесс: обновление при старте, затем
// ежедневно в 00:10 и 01:10 UTC. Второй прогон идемпотентен —
// условный upsert перезапишет только если данные выросли.
func Run(
	ctx context.Context,
	updateFunc func(ctx context.Context, date time.Time) error,
	retryInterval time.Duration,
) {
	if retryInterval <= 0 {
		retryInterval = 30 * time.Minute
	}

	// Первый прогон при старте (с догоняющими попытками).
	runWithRetries(ctx, updateFunc, retryInterval, "initial")

	for {
		wait := untilNextRun()
		log.Printf("Scheduler: next update in %v (at %s UTC)",
			wait, time.Now().UTC().Add(wait).Format(time.RFC3339))

		select {
		case <-ctx.Done():
			log.Println("Scheduler: stopped")
			return
		case <-time.After(wait):
			runWithRetries(ctx, updateFunc, retryInterval, "scheduled")
		}
	}
}

// runWithRetries вызывает updateFunc для «вчера», и если тот вернул ошибку —
// ждёт retryInterval и пробует снова. Продолжается до успеха или отмены ctx.
func runWithRetries(
	ctx context.Context,
	updateFunc func(ctx context.Context, date time.Time) error,
	retryInterval time.Duration,
	label string,
) {
	attempt := 0
	for {
		attempt++
		date := timeutil.YesterdayUTC()

		err := updateFunc(ctx, date)
		if err == nil {
			log.Printf("Scheduler: %s update for %s completed (attempt %d)",
				label, date.Format("2006-01-02"), attempt)
			return
		}

		log.Printf("Scheduler: %s update for %s failed (attempt %d), retrying in %v: %v",
			label, date.Format("2006-01-02"), attempt, retryInterval, err)

		select {
		case <-ctx.Done():
			log.Printf("Scheduler: %s retry loop stopped by context", label)
			return
		case <-time.After(retryInterval):
		}
	}
}

// untilNextRun возвращает время до ближайшего момента 00:10 / 01:10 UTC.
func untilNextRun() time.Duration {
	return untilNextRunFrom(time.Now().UTC())
}

// untilNextRunFrom — чистая функция, тестируемая.
func untilNextRunFrom(now time.Time) time.Duration {
	candidates := []time.Time{
		time.Date(now.Year(), now.Month(), now.Day(), 0, 10, 0, 0, time.UTC),
		time.Date(now.Year(), now.Month(), now.Day(), 1, 10, 0, 0, time.UTC),
	}
	for _, c := range candidates {
		if c.After(now) {
			return c.Sub(now)
		}
	}
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 10, 0, 0, time.UTC)
	return next.Sub(now)
}
