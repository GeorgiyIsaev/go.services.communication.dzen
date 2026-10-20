package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"go.services.communication.dzen/app/ClickImitator/internal/client"
	"go.services.communication.dzen/app/ClickImitator/internal/service"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
	"go.services.communication.dzen/app/ClickImitator/pkg/logger"
)

func main() {
	cfg := config.Load()
	appLogger := logger.New(cfg.LogLevel)

	if err := run(cfg, appLogger); err != nil {
		appLogger.Error("service stopped with error", "err", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config, appLogger *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sender, err := client.NewKafkaClickSender(cfg.Kafka.Brokers, cfg.Kafka.Topic)
	if err != nil {
		return err
	}
	defer func() {
		if err := sender.Close(); err != nil {
			appLogger.Error("kafka close error", "err", err)
		}
	}()

	strategy, err := buildStrategy(cfg, appLogger)
	if err != nil {
		return err
	}

	if err := service.NewSimulator(strategy, sender, cfg.Sender, appLogger).Run(ctx); err != nil {
		return err
	}

	appLogger.Info("shutdown complete")
	return nil
}

func buildStrategy(cfg *config.Config, appLogger *slog.Logger) (service.Strategy, error) {
	switch cfg.Mode {
	case config.ModeRandom:
		return service.NewRandomStrategy(cfg.Random, appLogger), nil

	case config.ModeScenario:
		scenarios, err := service.LoadScenarios(cfg.Scenario.Dir)
		if err != nil {
			return nil, err
		}
		appLogger.Info("scenarios loaded", "dir", cfg.Scenario.Dir, "count", len(scenarios))
		return service.NewScenarioStrategy(cfg.Scenario, scenarios, appLogger), nil
	}
	return nil, fmt.Errorf("unknown generator mode %q", cfg.Mode)
}
