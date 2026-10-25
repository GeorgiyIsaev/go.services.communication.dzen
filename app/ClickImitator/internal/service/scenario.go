package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

type ScenarioStrategy struct {
	cfg       config.Scenario
	scenarios []domain.Scenario
	logger    *slog.Logger
	completed atomic.Int64
	failed    atomic.Int64
}

func NewScenarioStrategy(cfg config.Scenario, scenarios []domain.Scenario, logger *slog.Logger) *ScenarioStrategy {
	return &ScenarioStrategy{cfg: cfg, scenarios: scenarios, logger: logger}
}

func (s *ScenarioStrategy) Name() string { return "scenario" }

func (s *ScenarioStrategy) TotalUsers() int64 { return int64(len(s.scenarios)) }

func (s *ScenarioStrategy) Run(ctx context.Context, sender domain.ClickSender, scfg config.Sender) error {
	total := len(s.scenarios)
	s.logger.Info("scenario mode", "users", total)

	var wg sync.WaitGroup
	for _, sc := range s.scenarios {
		wg.Add(1)
		go s.runReader(ctx, sender, scfg, sc, &wg)
	}
	wg.Wait()

	if n := s.failed.Load(); n > 0 {
		return fmt.Errorf("%d send(s) failed", n)
	}
	return nil
}

func (s *ScenarioStrategy) runReader(
	ctx context.Context,
	sender domain.ClickSender,
	scfg config.Sender,
	sc domain.Scenario,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer func() {
		n := s.completed.Add(1)
		s.logger.Info("reader finished",
			"reader_id", sc.UserID,
			"completed", n,
			"total", s.TotalUsers())
	}()

	var readWg sync.WaitGroup
	for _, authorID := range sc.Authors {
		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickMessage{
				UserID:    sc.UserID,
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
