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

	"go.services.communication.dzen/app/StorageGateway/internal/handlers"
	"go.services.communication.dzen/app/StorageGateway/internal/middleware"
	"go.services.communication.dzen/app/StorageGateway/internal/repository"
	"go.services.communication.dzen/app/StorageGateway/internal/service"
	"go.services.communication.dzen/app/StorageGateway/pkg/config"
	"go.services.communication.dzen/app/StorageGateway/pkg/db"
	"go.services.communication.dzen/app/StorageGateway/pkg/logger"
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

	database, err := db.Connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()

	repo := repository.NewPostgresRepository(database)
	svc := service.NewStorageService(repo)
	handler := handlers.NewStorageHandler(svc, appLogger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// Порядок: RequestID → Logging → mux.
	// RequestID снаружи, чтобы request_id попал в ctx до того, как Logging
	// начнёт писать access-log. Затем Logging, затем — сами хендлеры.
	var h http.Handler = mux
	h = middleware.Logging(appLogger)(h)
	h = middleware.RequestID(h)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("StorageGateway started", "port", cfg.ServerPort)
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
