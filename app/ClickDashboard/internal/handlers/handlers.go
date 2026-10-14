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
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// days
	const (
		defaultDays = 5
		maxDays     = 30
	)
	days := defaultDays
	if daysStr := r.URL.Query().Get("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			if d > maxDays {
				d = maxDays
			}
			days = d
		}
	}

	// limit
	const (
		defaultLimit = 100
		maxLimit     = 1000
	)
	limit := defaultLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			if l > maxLimit {
				l = maxLimit
			}
			limit = l
		}
	}

	// offset
	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	log.Printf("Received request for %d days (limit=%d, offset=%d)", days, limit, offset)

	resp, err := h.service.GetStats(days, limit, offset) // ← вот фикс
	if err != nil {
		log.Printf("Error fetching stats: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding JSON: %v", err)
	}
}
