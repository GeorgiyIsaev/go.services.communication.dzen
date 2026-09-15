package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/service"
)

type StatsResponse struct {
	Stats []AuthorStat `json:"stats"`
}

type AuthorStat struct {
	AuthorID int `json:"author_id"`
	Count    int `json:"count"`
}

type Handler struct {
	repo service.Repository
}

func New(repo service.Repository) *Handler {
	return &Handler{repo: repo}
}

// GetStatsHandler возвращает статистику за указанную дату (по умолчанию за вчера).
// GET /stats
func (h *Handler) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	y, m, d := now.AddDate(0, 0, -1).Date()
	yesterday := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	log.Printf("Handler: GET /stats for date %s", yesterday.Format("2006-01-02"))

	stats, err := h.repo.GetStatsForDate(r.Context(), yesterday)
	if err != nil {
		log.Printf("Handler: error getting stats: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := StatsResponse{Stats: make([]AuthorStat, 0, len(stats))}
	for id, count := range stats {
		resp.Stats = append(resp.Stats, AuthorStat{AuthorID: id, Count: count})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Handler: encode error: %v", err)
	}
}
