package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/timeutil"
)

type StatsResponse struct {
	Stats []AuthorStat `json:"stats"`
}

type AuthorStat struct {
	AuthorID int `json:"author_id"`
	Count    int `json:"count"`
}

type Repository interface {
	GetStatsForDate(ctx context.Context, date time.Time) (map[int]int, error)
}

type Handler struct {
	repo Repository
}

func New(repo Repository) *Handler {
	return &Handler{repo: repo}
}

// GetStatsHandler возвращает статистику за вчерашний день (UTC).
func (h *Handler) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	yesterday := timeutil.YesterdayUTC()
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
