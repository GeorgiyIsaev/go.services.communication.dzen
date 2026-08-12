// internal/handler/handler_test.go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.services.communication.dzen/internal/ClickStorage_InMemory/service"
	"go.services.communication.dzen/internal/ClickStorage_InMemory/storage"
)

// ====== МОК СЕРВИСА ======
type mockStatsService struct {
	updateErr error
	stats     map[int]int
}

func (m *mockStatsService) UpdateStats(ctx context.Context) error {
	return m.updateErr
}
func (m *mockStatsService) GetStats() map[int]int {
	return m.stats
}
func (m *mockStatsService) AddAuthor(ctx context.Context, id int) error {
	return nil
}
func (m *mockStatsService) RemoveAuthor(ctx context.Context, id int) error {
	return nil
}

// ====== ТЕСТ 1: с моком ======
func TestUpdateStatsHandler_Mock(t *testing.T) {
	tests := []struct {
		name       string
		mockErr    error
		wantStatus int
	}{
		{"success", nil, http.StatusOK},
		{"service error", context.DeadlineExceeded, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSvc := &mockStatsService{updateErr: tt.mockErr}
			h := NewHandler(mockSvc)

			req := httptest.NewRequest(http.MethodPost, "/update", nil)
			rr := httptest.NewRecorder()

			h.UpdateStatsHandler(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

// ====== ТЕСТ 2: интеграционный (реальный внешний сервер) ======
func TestUpdateStatsHandler_Real(t *testing.T) {
	// 1. Поднимаем тестовый внешний сервер, имитирующий /stats
	mux := http.NewServeMux()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		// Проверяем параметр author_ids
		ids := r.URL.Query().Get("author_ids")
		if ids == "" {
			http.Error(w, "missing author_ids", http.StatusBadRequest)
			return
		}
		// Возвращаем фиктивную статистику для всех запрошенных id
		// Для простоты возвращаем {"1": 10, "2": 20, "3": 30}
		resp := map[string]int{"1": 10, "2": 20, "3": 30}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	externalSrv := httptest.NewServer(mux)
	defer externalSrv.Close()

	// 2. Создаём реальный репозиторий и сервис с URL тестового сервера
	repo := storage.NewInMemoryRepo()
	ctx := context.Background()
	_ = repo.AddAuthor(ctx, 1)
	_ = repo.AddAuthor(ctx, 2)
	_ = repo.AddAuthor(ctx, 3)

	svc := service.NewStatsService(repo, externalSrv.URL+"/stats")
	h := NewHandler(svc)

	// 3. Делаем запрос к нашему обработчику
	req := httptest.NewRequest(http.MethodPost, "/update", nil)
	rr := httptest.NewRecorder()
	h.UpdateStatsHandler(rr, req)

	// 4. Проверяем статус
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// 5. Проверяем, что статистика сохранилась в сервисе
	stats := svc.GetStats()
	expected := map[int]int{1: 10, 2: 20, 3: 30}
	for id, clicks := range expected {
		if stats[id] != clicks {
			t.Errorf("for author %d expected %d, got %d", id, clicks, stats[id])
		}
	}
}
