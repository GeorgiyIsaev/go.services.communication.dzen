// internal/StatsKeeper/scheduler/scheduler.go
package scheduler

import (
	"context"
	"log"
	"time"
)

// Run запускает фоновый процесс: выполняет обновление для вчерашнего дня
// при старте, затем ждёт до полуночи и повторяет.
func Run(ctx context.Context, updateFunc func(ctx context.Context, date time.Time) error) {
	// Функция для вычисления времени до следующей полуночи
	nextMidnight := func() time.Duration {
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		return midnight.Sub(now)
	}

	// Первое обновление: статистика за вчерашний день
	yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	log.Printf("Scheduler: initial update for date %s", yesterday.Format("2006-01-02"))
	if err := updateFunc(ctx, yesterday); err != nil {
		log.Printf("Scheduler: initial update failed: %v", err)
	}

	// Цикл обновлений каждую полночь
	for {
		wait := nextMidnight()
		log.Printf("Scheduler: next update in %v", wait)

		select {
		case <-ctx.Done():
			log.Println("Scheduler: stopped")
			return
		case <-time.After(wait):
			// Наступила полночь – обновляем статистику за вчера
			yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
			log.Printf("Scheduler: daily update for date %s", yesterday.Format("2006-01-02"))
			if err := updateFunc(ctx, yesterday); err != nil {
				log.Printf("Scheduler: daily update failed: %v", err)
			}
		}
	}
}
