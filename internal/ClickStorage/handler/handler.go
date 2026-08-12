package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/repository"
	"go.services.communication.dzen/internal/ClickStorage/service"
)

type Handler struct {
	repo    repository.Repository
	service *service.Service
}

func New(repo repository.Repository, svc *service.Service) *Handler {
	return &Handler{repo: repo, service: svc}
}

// GetStatsHandler возвращает статистику за указанную дату (по умолчанию за вчера).
// GET /stats?date=2025-01-01
func (h *Handler) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	var date time.Time
	var err error
	if dateStr == "" {
		// По умолчанию вчера
		date = time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}

	// Получаем статистику из БД (простой запрос, можно реализовать в репозитории)
	stats, err := h.repo.GetStatsForDate(r.Context(), date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// UpdateHandler принудительно обновляет статистику за вчерашний день.
// POST /update
func (h *Handler) UpdateHandler(w http.ResponseWriter, r *http.Request) {
	yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	if err := h.service.UpdateStatsForDate(r.Context(), yesterday); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Stats updated for " + yesterday.Format("2006-01-02")))
}
