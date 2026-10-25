package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/client"
	"go.services.communication.dzen/app/ClickDashboard/internal/handlers"
	"go.services.communication.dzen/app/ClickDashboard/internal/service"
	"go.services.communication.dzen/app/ClickDashboard/pkg/config"
	"go.services.communication.dzen/app/ClickDashboard/pkg/logger"
	"go.services.communication.dzen/app/ClickDashboard/pkg/middleware"
)

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

	// gRPC-клиент к StorageGateway.
	sgClient, err := client.NewStorageGatewayClient(cfg.Gateway.Addr, cfg.Gateway.Timeout)
	if err != nil {
		return fmt.Errorf("storage gateway client: %w", err)
	}
	defer sgClient.Close()

	store := service.NewStore()
	svc := service.NewStatsService(sgClient, store, appLogger, cfg.Projection)

	// refresh-цикл для проекции
	go svc.Run(ctx)

	// Хендлеры.
	h := handlers.NewStatsHandler(svc, appLogger)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	var handler http.Handler = mux
	handler = middleware.Recover(appLogger)(handler)
	handler = middleware.Logging(appLogger)(handler)
	handler = middleware.RequestID(handler)

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       35 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("ClickDashboard started",
			"port", cfg.Server.Port,
			"storage_gateway_addr", cfg.Gateway.Addr,
			"max_days", cfg.Projection.MaxDays,
			"refresh_interval", cfg.Projection.RefreshInterval.String())
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
