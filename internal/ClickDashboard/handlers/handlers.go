package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"strconv"

	"go.services.communication.dzen/internal/ClickDashboard/service"
)

type StatsHandler struct {
	service *service.StatsService
}

func NewStatsHandler(db *sql.DB) *StatsHandler {
	return &StatsHandler{
		service: service.NewStatsService(db),
	}
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	// Разрешаем CORS для всех источников
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	daysStr := r.URL.Query().Get("days")
	days := 5
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}
	log.Printf("Received request for %d days", days)

	resp, err := h.service.GetStats(days)
	if err != nil {
		log.Printf("Error fetching stats: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
