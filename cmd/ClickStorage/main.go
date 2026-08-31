// cmd/StatsKeeper/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/config"
	"go.services.communication.dzen/internal/ClickStorage/db"
	"go.services.communication.dzen/internal/ClickStorage/handler"
	"go.services.communication.dzen/internal/ClickStorage/repository"
	"go.services.communication.dzen/internal/ClickStorage/scheduler"
	"go.services.communication.dzen/internal/ClickStorage/service"
)

func main() {
	// 1. Загрузка конфигурации
	cfg := config.Load()

	// 2. Подключение к БД
	database, err := db.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}
	defer database.Close()

	// 3. Репозиторий
	repo := repository.New(database)

	// 4. Сервис
	svc := service.New(repo, cfg.StatsURL)

	// 5. HTTP-обработчики
	h := handler.New(repo, svc)

	// 6. HTTP-сервер
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", h.GetStatsHandler)
	mux.HandleFunc("POST /update", h.UpdateHandler)

	srv := &http.Server{
		Addr:    ":8084",
		Handler: mux,
	}

	// 7. Контекст для планировщика
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 8. Запуск планировщика в фоне
	go scheduler.Run(ctx, svc.UpdateStatsForDate)

	// 9. Запуск HTTP-сервера в горутине
	go func() {
		log.Printf("Starting HTTP server on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 10. Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down gracefully...")
	cancel() // остановка планировщика

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
