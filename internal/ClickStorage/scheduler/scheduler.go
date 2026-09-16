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
func Run(ctx context.Context, updateFunc func(ctx context.Context, date time.Time) error) {
	runUpdate := func(label string) {
		yesterday := timeutil.YesterdayUTC()
		log.Printf("Scheduler: %s update for date %s", label, yesterday.Format("2006-01-02"))
		if err := updateFunc(ctx, yesterday); err != nil {
			log.Printf("Scheduler: %s update failed: %v", label, err)
		} else {
			log.Printf("Scheduler: %s update completed", label)
		}
	}

	// Первое обновление при старте
	runUpdate("initial")

	for {
		wait := untilNextRun()
		log.Printf("Scheduler: next update in %v", wait)

		select {
		case <-ctx.Done():
			log.Println("Scheduler: stopped")
			return
		case <-time.After(wait):
			runUpdate("scheduled")
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
