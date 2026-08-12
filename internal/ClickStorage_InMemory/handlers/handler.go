// internal/handler/handler.go
package handlers

import (
	"encoding/json"
	"net/http"

	"go.services.communication.dzen/internal/ClickStorage_InMemory/service"
)

type Handler struct {
	svc service.StatsService
}

func NewHandler(svc service.StatsService) *Handler {
	return &Handler{svc: svc}
}

// UpdateStatsHandler – эндпоинт для принудительного обновления статистики.
// GET /update (или POST – по желанию)
func (h *Handler) UpdateStatsHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.UpdateStats(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Stats updated successfully"))
}

// GetStatsHandler – возвращает текущую статистику.
func (h *Handler) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	stats := h.svc.GetStats()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// AddAuthorHandler – добавляет автора (пример дополнительного эндпоинта).
// Можно не включать в main, если не требуется.
// Оставлен для демонстрации.
func (h *Handler) AddAuthorHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := h.svc.AddAuthor(r.Context(), req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// RemoveAuthorHandler – удаляет автора.
func (h *Handler) RemoveAuthorHandler(w http.ResponseWriter, r *http.Request) {
	// Пример: /authors/{id}
	// Реализация опущена для краткости
}
