package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/config"
	"go.services.communication.dzen/internal/ClickStorage/db"
	"go.services.communication.dzen/internal/ClickStorage/repository"
	"go.services.communication.dzen/internal/ClickStorage/scheduler"
	"go.services.communication.dzen/internal/ClickStorage/service"
)

func main() {
	// 1. Конфигурация
	cfg := config.Load()

	// 2. БД
	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer database.Close()

	// 3. Репозиторий (реализует и service.Repository, и handler.Repository)
	repo := repository.New(database)

	// 4. Сервис обновления статистики
	svc := service.New(repo, cfg.StatsURL, cfg.StatsBatchSize, cfg.StatsMaxRetries)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// 5. Контекст, который отменится по SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 6. Планировщик
	go scheduler.Run(ctx, svc.UpdateStatsForDate, cfg.SchedulerRetryInterval)

	// 7. HTTP-сервер
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Starting HTTP server on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// 8. Ждём сигнал или падение сервера.
	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received")
	case err := <-serverErr:
		log.Fatalf("Server failed: %v", err)
	}

	// 9. Graceful shutdown
	stop() // восстановить поведение сигналов по умолчанию

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
