package usecase

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.services.communication.dzen/internal/ClickScenario/domain"
)

type Simulator struct {
	cfg              domain.Config
	sender           domain.ClickSender
	scenarios        []domain.Scenario
	completedReaders int64
	totalReaders     int64
}

func NewSimulator(cfg domain.Config, sender domain.ClickSender, scenarios []domain.Scenario) *Simulator {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 1
	}
	return &Simulator{
		cfg:          cfg,
		sender:       sender,
		scenarios:    scenarios,
		totalReaders: int64(len(scenarios)),
	}
}

// Run запускает по одной горутине на каждого пользователя из сценариев.
// Все пользователи работают параллельно, каждый — независимо.
func (s *Simulator) Run(ctx context.Context) {
	fmt.Printf("Запуск сценариев. Всего пользователей: %d\n", s.totalReaders)

	var mainWg sync.WaitGroup
	for _, sc := range s.scenarios {
		mainWg.Add(1)
		go s.runReaderGoroutine(ctx, sc, &mainWg)
	}

	if ctx.Err() != nil {
		fmt.Println("Сценарии прерваны. Программа завершает работу.")
		return
	}

	mainWg.Wait()
	fmt.Println("Все сценарии выполнены. Программа завершает работу.")
}

// runReaderGoroutine — горутина одного пользователя.
// Читает авторов строго в порядке, заданном в слайсе сценария,
// но каждое чтение запускает в отдельной внутренней горутине.
func (s *Simulator) runReaderGoroutine(ctx context.Context, sc domain.Scenario, mainWg *sync.WaitGroup) {
	defer mainWg.Done()

	defer func() {
		completed := atomic.AddInt64(&s.completedReaders, 1)
		fmt.Printf("Сценарий пользователя %d завершён (%d/%d)\n",
			sc.UserID, completed, s.totalReaders)
	}()

	var readWg sync.WaitGroup

	for _, authorID := range sc.Authors {
		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickRequest{
				UserID:   sc.UserID,
				AuthorID: aID,
			}
			s.sendWithRetry(ctx, req)
		}(authorID)

		if s.cfg.DelayBetweenReadsSec > 0 {
			delay := time.Duration(s.cfg.DelayBetweenReadsSec) * time.Second
			if !sleepWithContext(ctx, delay) {
				break
			}
		}
	}

	readWg.Wait()
}

func (s *Simulator) sendWithRetry(ctx context.Context, req domain.ClickRequest) {
	maxRetries := s.cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.sender.Send(sendCtx, req)
		cancel()

		if err == nil {
			return
		}

		if ctx.Err() != nil {
			fmt.Printf("[WARN] Отправка прервана (User: %d, Author: %d): %v\n",
				req.UserID, req.AuthorID, ctx.Err())
			return
		}

		fmt.Printf("[WARN] Не удалось отправить клик (User: %d, Author: %d). Попытка %d/%d. Ошибка: %v\n",
			req.UserID, req.AuthorID, attempt, maxRetries, err)

		if attempt < maxRetries {
			half := int64(backoff / 2)
			jitter := time.Duration(0)
			if half > 0 {
				jitter = time.Duration(rand.Int63n(half))
			}

			if !sleepWithContext(ctx, backoff+jitter) {
				return
			}

			backoff *= 2
		}
	}

	fmt.Printf("[ERROR] Клик окончательно не доставлен после %d попыток (User: %d, Author: %d)\n",
		maxRetries, req.UserID, req.AuthorID)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
