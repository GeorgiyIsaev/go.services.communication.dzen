package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

// ScenarioStrategy — воспроизведение заранее заданных сценариев.
type ScenarioStrategy struct {
	cfg       config.Scenario
	scenarios []domain.Scenario
	logger    *slog.Logger
}

func NewScenarioStrategy(cfg config.Scenario, scenarios []domain.Scenario, logger *slog.Logger) *ScenarioStrategy {
	return &ScenarioStrategy{cfg: cfg, scenarios: scenarios, logger: logger}
}

func (s *ScenarioStrategy) Name() string { return "scenario" }

func (s *ScenarioStrategy) TotalUsers() int64 {
	return int64(len(s.scenarios))
}

func (s *ScenarioStrategy) Run(ctx context.Context, sender domain.ClickSender, maxRetries int) error {
	total := len(s.scenarios)
	s.logger.Info("scenario mode", "users", total)

	var mainWg sync.WaitGroup
	for _, sc := range s.scenarios {
		mainWg.Add(1)
		go s.runReader(ctx, sender, maxRetries, sc, &mainWg)
	}
	mainWg.Wait()
	return nil
}

func (s *ScenarioStrategy) runReader(
	ctx context.Context,
	sender domain.ClickSender,
	maxRetries int,
	sc domain.Scenario,
	mainWg *sync.WaitGroup,
) {
	defer mainWg.Done()

	var readWg sync.WaitGroup
	for _, authorID := range sc.Authors {
		readWg.Add(1)
		go func(aID int64) {
			defer readWg.Done()
			req := domain.ClickEvent{
				UserID:    sc.UserID,
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
