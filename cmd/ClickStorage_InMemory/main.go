// main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"go.services.communication.dzen/internal/ClickStorage_InMemory/handlers"
	"go.services.communication.dzen/internal/ClickStorage_InMemory/service"
	"go.services.communication.dzen/internal/ClickStorage_InMemory/storage"
)

func main() {
	// 1. Репозиторий (in-memory)
	repo := storage.NewInMemoryRepo()

	// 2. Добавим тестовых авторов (можно загружать из переменных окружения)
	ctx := context.Background()
	_ = repo.AddAuthor(ctx, 1)
	_ = repo.AddAuthor(ctx, 2)
	_ = repo.AddAuthor(ctx, 3)

	// 3. URL внешнего сервиса (из переменной окружения или по умолчанию)
	statsURL := os.Getenv("STATS_SERVICE_URL")
	if statsURL == "" {
		statsURL = "http://external-service/stats"
	}

	// 4. Сервис
	svc := service.NewStatsService(repo, statsURL)

	// 5. Обработчик
	h := handlers.NewHandler(svc)

	// 6. Маршруты (только нужный эндпоинт)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /update", h.UpdateStatsHandler) // или GET /update

	// Запуск
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
