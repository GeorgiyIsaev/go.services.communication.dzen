package usecase

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.services.communication.dzen/internal/ClickGenerator/domain"
)

type Simulator struct {
	cfg              domain.Config
	sender           domain.ClickSender // Зависимость от интерфейса, а не от конкретной реализации
	completedReaders int64              // Атомарный счетчик завершенных горутин пользователей
	totalReaders     int64
	rng              *rand.Rand
}

func NewSimulator(cfg domain.Config, sender domain.ClickSender) *Simulator {
	totalReaders := cfg.ReaderIDEnd - cfg.ReaderIDStart + 1
	return &Simulator{
		cfg:          cfg,
		sender:       sender,
		totalReaders: totalReaders,
		rng:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *Simulator) Run() {
	fmt.Printf("Запуск симуляции. Всего пользователей: %d\n", s.totalReaders)

	var mainWg sync.WaitGroup

	for readerID := s.cfg.ReaderIDStart; readerID <= s.cfg.ReaderIDEnd; readerID++ {
		mainWg.Add(1)
		go s.runReaderGoroutine(readerID, &mainWg)
	}

	mainWg.Wait()
	fmt.Println("Все горутины успешно завершены. Программа завершает работу.")
}

func (s *Simulator) runReaderGoroutine(readerID int64, mainWg *sync.WaitGroup) {
	defer mainWg.Done()

	defer func() {
		completed := atomic.AddInt64(&s.completedReaders, 1)
		fmt.Printf("Горутина Завершена %d из %d (User ID: %d)\n", completed, s.totalReaders, readerID)
	}()

	numReads := s.rng.Intn(s.cfg.MaxReads-s.cfg.MinReads+1) + s.cfg.MinReads
	var readWg sync.WaitGroup

	for i := 0; i < numReads; i++ {
		authorID := s.rng.Int63n(s.cfg.AuthorIDEnd-s.cfg.AuthorIDStart+1) + s.cfg.AuthorIDStart

		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickRequest{UserID: readerID, AuthorID: aID}
			s.sendWithRetry(req)
		}(authorID)

		if s.cfg.DelayBetweenReadsSec > 0 {
			time.Sleep(time.Duration(s.cfg.DelayBetweenReadsSec) * time.Second)
		}
	}

	// Горутина пользователя ждет завершения всех внутренних горутин отправки (включая ретраи)
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
			return // Успешно отправлено
		}

		fmt.Printf("[WARN] Не удалось отправить клик (User: %d, Author: %d). Попытка %d/%d. Ошибка: %v\n",
			req.UserID, req.AuthorID, attempt, maxRetries, err)

		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2 // Экспоненциальное увеличение задержки (Exponential Backoff)
		}
	}

	fmt.Printf("[ERROR] Клик окончательно не доставлен после %d попыток (User: %d, Author: %d)\n",
		maxRetries, req.UserID, req.AuthorID)
}
