package main

import (
	"context"
	"errors"
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
	appLogger := logger.New(slog.LevelInfo)

	if err := run(appLogger); err != nil {
		appLogger.Error("service stopped with error", "err", err)
		os.Exit(1)
	}
}

func run(appLogger *slog.Logger) error {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Создаём топик с retention 48ч.
	if err := client.EnsureTopic(
		cfg.Kafka.Brokers,
		cfg.Kafka.Topic,
		cfg.Kafka.Partitions,
		cfg.Kafka.ReplicationFactor,
		cfg.Kafka.Retention,
		cfg.Kafka.Segment,
	); err != nil {
		return err
	}
	appLogger.Info("topic ready",
		"topic", cfg.Kafka.Topic,
		"partitions", cfg.Kafka.Partitions,
		"rf", cfg.Kafka.ReplicationFactor,
		"retention", cfg.Kafka.Retention.String(),
		"segment", cfg.Kafka.Segment.String())

	// 2. Kafka sender.
	sender, err := client.NewKafkaClickSender(cfg.Kafka.Brokers, cfg.Kafka.Topic)
	if err != nil {
		return err
	}
	defer func() {
		if err := sender.Close(); err != nil {
			appLogger.Error("kafka close error", "err", err)
		}
	}()

	// 3. Стратегия в зависимости от режима.
	strategy, err := buildStrategy(cfg, appLogger)
	if err != nil {
		return err
	}

	// 4. Симулятор.
	sim := service.NewSimulator(strategy, sender, cfg.Sender.MaxRetries, appLogger)
	sim.Run(ctx)

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
		appLogger.Info("scenarios loaded",
			"dir", cfg.Scenario.Dir, "count", len(scenarios))
		return service.NewScenarioStrategy(cfg.Scenario, scenarios, appLogger), nil

	default:
		// config.Load уже гарантирует валидный режим, но на всякий случай.
		return nil, errors.New("unknown generator mode: " + string(cfg.Mode))
	}
}
