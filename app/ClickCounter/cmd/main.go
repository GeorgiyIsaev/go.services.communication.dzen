package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/client"
	"go.services.communication.dzen/app/ClickCounter/internal/handlers"
	"go.services.communication.dzen/app/ClickCounter/internal/service"
	"go.services.communication.dzen/app/ClickCounter/pkg/config"
	"go.services.communication.dzen/app/ClickCounter/pkg/logger"
	"go.services.communication.dzen/app/ClickCounter/pkg/middleware"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Tracker — общая in-memory память для кликов.
	tracker := service.NewTracker()

	// 2. HTTP-клиент к StorageGateway.
	sgClient := client.NewStorageGatewayClient(cfg.Gateway.URL, cfg.Gateway.Timeout)

	// 3. Sender — отправка с ретраями.
	sender := service.NewSender(tracker, sgClient, appLogger, cfg.Retries)
	defer sender.Shutdown()

	// 4. Kafka-консьюмер (без группы, читаем с начала).
	consumer := service.NewConsumer(cfg.Kafka, tracker, appLogger)
	defer consumer.Close()

	go func() {
		if err := consumer.Run(ctx); err != nil {
			appLogger.Error("consumer stopped", "err", err)
		}
	}()

	// 5. Scheduler — отправка в 00:05 и 01:05 UTC.
	go service.RunScheduler(ctx, sender, appLogger, cfg.Scheduler)

	// 5a. Cleanup — очистка старых дней раз в сутки в 00:00 UTC.
	go service.RunCleanup(ctx, tracker, appLogger)

	// 6. HTTP-сервер.
	mux := http.NewServeMux()
	h := handlers.NewStatsHandler(sender, tracker, appLogger)
	h.RegisterRoutes(mux)

	var handler http.Handler = mux
	handler = middleware.Recover(appLogger)(handler)
	handler = middleware.Logging(appLogger)(handler)
	handler = middleware.RequestID(handler)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTP.Port,
		Handler:           handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("ClickCounter started",
			"port", cfg.HTTP.Port,
			"kafka_topic", cfg.Kafka.Topic,
			"storage_gateway_url", cfg.Gateway.URL,
			"scheduler_hours", cfg.Scheduler.Hours,
			"scheduler_minute", cfg.Scheduler.Minute)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		appLogger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
