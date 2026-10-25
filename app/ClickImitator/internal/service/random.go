package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

type RandomStrategy struct {
	cfg       config.Random
	logger    *slog.Logger
	completed atomic.Int64
	failed    atomic.Int64
}

func NewRandomStrategy(cfg config.Random, logger *slog.Logger) *RandomStrategy {
	return &RandomStrategy{cfg: cfg, logger: logger}
}

func (s *RandomStrategy) Name() string { return "random" }

func (s *RandomStrategy) TotalUsers() int64 {
	return s.cfg.ReaderIDEnd - s.cfg.ReaderIDStart + 1
}

func (s *RandomStrategy) Run(ctx context.Context, sender domain.ClickSender, scfg config.Sender) error {
	total := s.TotalUsers()
	s.logger.Info("random mode", "readers", total)

	var wg sync.WaitGroup
	for readerID := s.cfg.ReaderIDStart; readerID <= s.cfg.ReaderIDEnd; readerID++ {
		wg.Add(1)
		go s.runReader(ctx, sender, scfg, readerID, &wg)
	}
	wg.Wait()

	if n := s.failed.Load(); n > 0 {
		return fmt.Errorf("%d send(s) failed", n)
	}
	return nil
}

func (s *RandomStrategy) runReader(
	ctx context.Context,
	sender domain.ClickSender,
	scfg config.Sender,
	readerID int64,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer func() {
		n := s.completed.Add(1)
		s.logger.Info("reader finished",
			"reader_id", readerID,
			"completed", n,
			"total", s.TotalUsers())
	}()

	numReads := rand.Intn(s.cfg.MaxReads-s.cfg.MinReads+1) + s.cfg.MinReads
	var readWg sync.WaitGroup

	for i := 0; i < numReads; i++ {
		authorID := rand.Int63n(s.cfg.AuthorIDEnd-s.cfg.AuthorIDStart+1) + s.cfg.AuthorIDStart
		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickMessage{
				UserID:    readerID,
				AuthorID:  aID,
				Timestamp: time.Now().UTC(),
			}
			if err := sendWithRetry(ctx, sender, req, scfg, s.logger); err != nil {
				s.failed.Add(1)
			}
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
