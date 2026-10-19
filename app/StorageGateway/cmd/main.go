package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"go.services.communication.dzen/app/StorageGateway/internal/handlers"
	"go.services.communication.dzen/app/StorageGateway/internal/repository"
	"go.services.communication.dzen/app/StorageGateway/internal/service"
	"go.services.communication.dzen/app/StorageGateway/pkg/config"
	"go.services.communication.dzen/app/StorageGateway/pkg/db"
	"go.services.communication.dzen/app/StorageGateway/pkg/logger"
	"go.services.communication.dzen/app/StorageGateway/pkg/middleware"
	"go.services.communication.dzen/app/StorageGateway/pkg/pb"
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

	// --- HTTP Сервер (для ClickCounter) ---
	httpHandler := handlers.NewStorageHandler(svc, appLogger)
	mux := http.NewServeMux()
	httpHandler.RegisterRoutes(mux)

	var h http.Handler = mux
	h = middleware.Recover(appLogger)(h)
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

	errCh := make(chan error, 2)

	go func() {
		appLogger.Info("StorageGateway HTTP started", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// --- gRPC Сервер (для ClickDashboard) ---
	grpcHandler := handlers.NewGRPCStorageHandler(svc)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.GRPCRequestID(),
			middleware.GRPCRecover(appLogger),
			middleware.GRPCLogging(appLogger),
		),
	)
	pb.RegisterStorageGatewayServer(grpcSrv, grpcHandler)

	go func() {
		lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
		if err != nil {
			errCh <- err
			return
		}
		appLogger.Info("StorageGateway gRPC started", "port", cfg.GRPCPort)
		if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
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

	// Останавливаем HTTP
	err = srv.Shutdown(shutdownCtx)

	// Останавливаем gRPC
	stopped := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		grpcSrv.Stop() // Форсированная остановка, если по таймауту не уложились
	}

	return err
}
