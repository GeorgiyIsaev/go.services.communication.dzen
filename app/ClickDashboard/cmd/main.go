package main

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/client"
	"go.services.communication.dzen/app/ClickDashboard/internal/handlers"
	"go.services.communication.dzen/app/ClickDashboard/internal/middleware"
	"go.services.communication.dzen/app/ClickDashboard/internal/service"
	"go.services.communication.dzen/app/ClickDashboard/pkg/config"
	"go.services.communication.dzen/app/ClickDashboard/pkg/logger"
)

// indexHTML встраивается в бинарь.
//
//go:embed index.html
var indexHTML []byte

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

	// HTTP-клиент к StorageGateway.
	sgClient := client.NewStorageGatewayClient(cfg.StorageGatewayURL, cfg.StorageGatewayTimeout)

	// Сервис агрегации: лимит дней из конфига, семафор на 5 параллельных запросов.
	svc := service.NewStatsService(sgClient, cfg.MaxDays, 5)

	// Хендлеры.
	h := handlers.NewStatsHandler(svc, appLogger)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Отдаём index.html по корню.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	// Порядок middleware: RequestID (внешний) → Logging → Recover → mux.
	var handler http.Handler = mux
	handler = middleware.Recover(appLogger)(handler)
	handler = middleware.Logging(appLogger)(handler)
	handler = middleware.RequestID(handler)

	srv := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: handler,
		// ReadHeaderTimeout / IdleTimeout — стандартные.
		// ReadTimeout / WriteTimeout = 35s, чуть больше внутреннего таймаута /stats (30s),
		// чтобы сервер успел вернуть клиенту 504, а не оборвал соединение раньше.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("ClickDashboard started",
			"port", cfg.ServerPort,
			"storage_gateway_url", cfg.StorageGatewayURL,
			"max_days", cfg.MaxDays)
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
