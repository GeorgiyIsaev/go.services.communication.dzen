package usecase

import (
	"context"
	"fmt"
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
	return &Simulator{
		cfg:          cfg,
		sender:       sender,
		scenarios:    scenarios,
		totalReaders: int64(len(scenarios)),
	}
}

// Run запускает по одной горутине на каждого пользователя из сценариев.
// Все пользователи работают параллельно, каждый — независимо.
func (s *Simulator) Run() {
	fmt.Printf("Запуск сценариев. Всего пользователей: %d\n", s.totalReaders)

	var mainWg sync.WaitGroup
	for _, sc := range s.scenarios {
		mainWg.Add(1)
		go s.runReaderGoroutine(sc, &mainWg)
	}

	mainWg.Wait()
	fmt.Println("Все сценарии выполнены. Программа завершает работу.")
}

// runReaderGoroutine — горутина одного пользователя.
// Читает авторов строго в порядке, заданном в слайсе сценария,
// но каждое чтение запускает в отдельной внутренней горутине.
func (s *Simulator) runReaderGoroutine(sc domain.Scenario, mainWg *sync.WaitGroup) {
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
			req := domain.ClickRequest{UserID: sc.UserID, AuthorID: aID}
			s.sendWithRetry(req)
		}(authorID)

		if s.cfg.DelayBetweenReadsSec > 0 {
			time.Sleep(time.Duration(s.cfg.DelayBetweenReadsSec) * time.Second)
		}
	}

	readWg.Wait()
}

func (s *Simulator) sendWithRetry(req domain.ClickRequest) {
	maxRetries := 5
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.sender.Send(ctx, req)
		cancel()

		if err == nil {
			return
		}

		fmt.Printf("[WARN] Не удалось отправить клик (User: %d, Author: %d). Попытка %d/%d. Ошибка: %v\n",
			req.UserID, req.AuthorID, attempt, maxRetries, err)

		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}

	fmt.Printf("[ERROR] Клик окончательно не доставлен после %d попыток (User: %d, Author: %d)\n",
		maxRetries, req.UserID, req.AuthorID)
}
