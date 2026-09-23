package usecase

import (
	"context"
	"fmt"
	"log"
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
}

func NewSimulator(cfg domain.Config, sender domain.ClickSender) (*Simulator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("некорректный конфиг симулятора: %w", err)
	}
	if sender == nil {
		return nil, fmt.Errorf("sender не задан")
	}

	totalReaders := cfg.ReaderIDEnd - cfg.ReaderIDStart + 1
	return &Simulator{
		cfg:          cfg,
		sender:       sender,
		totalReaders: totalReaders,
	}, nil
}

func (s *Simulator) Run(ctx context.Context) {
	log.Printf("Запуск симуляции. Всего пользователей: %d\n", s.totalReaders)

	var mainWg sync.WaitGroup

	for readerID := s.cfg.ReaderIDStart; readerID <= s.cfg.ReaderIDEnd; readerID++ {
		mainWg.Add(1)
		go s.runReaderGoroutine(ctx, readerID, &mainWg)
	}

	mainWg.Wait()
	log.Println("Все горутины успешно завершены. Программа завершает работу.")
}

func (s *Simulator) runReaderGoroutine(ctx context.Context, readerID int64, mainWg *sync.WaitGroup) {
	defer mainWg.Done()

	defer func() {
		completed := atomic.AddInt64(&s.completedReaders, 1)
		log.Printf("Горутина Завершена %d из %d (User ID: %d)\n", completed, s.totalReaders, readerID)
	}()

	numReads := rand.Intn(s.cfg.MaxReads-s.cfg.MinReads+1) + s.cfg.MinReads
	var readWg sync.WaitGroup

	for i := 0; i < numReads; i++ {
		authorID := rand.Int63n(s.cfg.AuthorIDEnd-s.cfg.AuthorIDStart+1) + s.cfg.AuthorIDStart

		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickEvent{
				UserID:    readerID,
				AuthorID:  aID,
				Timestamp: time.Now().UTC()}
			s.sendWithRetry(ctx, req)
		}(authorID)

		if s.cfg.DelayBetweenReadsSec > 0 {
			select {
			case <-time.After(time.Duration(s.cfg.DelayBetweenReadsSec) * time.Second):
			case <-ctx.Done():
				readWg.Wait()
				return
			}
		}
	}

	// Горутина пользователя ждет завершения всех внутренних горутин отправки (включая ретраи)
	readWg.Wait()
}

func (s *Simulator) sendWithRetry(ctx context.Context, req domain.ClickEvent) {
	maxRetries := s.cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			log.Printf("[INFO] Отправка прервана (User: %d, Author: %d): %v\n", req.UserID, req.AuthorID, err)
			return
		}

		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.sender.Send(sendCtx, req)
		cancel()

		if err == nil {
			return // Успешно отправлено
		}

		log.Printf("[WARN] Не удалось отправить клик (User: %d, Author: %d). Попытка %d/%d. Ошибка: %v\n",
			req.UserID, req.AuthorID, attempt, maxRetries, err)

		if attempt < maxRetries {
			jitter := time.Duration(rand.Int63n(int64(backoff/2) + 1))
			select {
			case <-time.After(backoff + jitter):
			case <-ctx.Done():
				log.Printf("[INFO] Ретрай прерван (User: %d, Author: %d): %v\n", req.UserID, req.AuthorID, ctx.Err())
				return
			}
			backoff *= 2 // Экспоненциальное увеличение задержки (Exponential Backoff)
		}
	}

	log.Printf("[ERROR] Клик окончательно не доставлен после %d попыток (User: %d, Author: %d)\n",
		maxRetries, req.UserID, req.AuthorID)
}
