package service

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

// RandomStrategy — генерация случайных кликов из диапазона.
type RandomStrategy struct {
	cfg    config.Random
	logger *slog.Logger
}

func NewRandomStrategy(cfg config.Random, logger *slog.Logger) *RandomStrategy {
	return &RandomStrategy{cfg: cfg, logger: logger}
}

func (s *RandomStrategy) Name() string { return "random" }

func (s *RandomStrategy) TotalUsers() int64 {
	return s.cfg.ReaderIDEnd - s.cfg.ReaderIDStart + 1
}

func (s *RandomStrategy) Run(ctx context.Context, sender domain.ClickSender, maxRetries int) error {
	totalReaders := s.TotalUsers()
	s.logger.Info("random mode", "readers", totalReaders)

	var mainWg sync.WaitGroup
	for readerID := s.cfg.ReaderIDStart; readerID <= s.cfg.ReaderIDEnd; readerID++ {
		mainWg.Add(1)
		go s.runReader(ctx, sender, maxRetries, readerID, &mainWg)
	}
	mainWg.Wait()
	return nil
}

func (s *RandomStrategy) runReader(
	ctx context.Context,
	sender domain.ClickSender,
	maxRetries int,
	readerID int64,
	mainWg *sync.WaitGroup,
) {
	defer mainWg.Done()

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
				Timestamp: time.Now().UTC(),
			}
			sendWithRetry(ctx, sender, req, maxRetries, s.logger)
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
	readWg.Wait()
}
