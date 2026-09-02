package handler

import (
	"encoding/json"
	"log"
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
		date = time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
	}
	log.Printf("Handler: GET /stats for date %s", date.Format("2006-01-02"))

	stats, err := h.repo.GetStatsForDate(r.Context(), date)
	if err != nil {
		log.Printf("Handler: error getting stats: %v", err)
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
	log.Printf("Handler: POST /update for date %s", yesterday.Format("2006-01-02"))
	if err := h.service.UpdateStatsForDate(r.Context(), yesterday); err != nil {
		log.Printf("Handler: update failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Stats updated for " + yesterday.Format("2006-01-02")))
}
