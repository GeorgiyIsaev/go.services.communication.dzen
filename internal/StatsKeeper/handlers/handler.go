package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"go.services.communication.dzen/internal/StatsKeeper/service"
)

// ClickRequest представляет тело запроса для /click.
type ClickRequest struct {
	UserID   int64 `json:"user_id"`
	AuthorID int64 `json:"author_id"`
}

// StatsResponse представляет ответ для /stats.
type StatsResponse struct {
	Stats []AuthorStat `json:"stats"`
}

// AuthorStat описывает статистику по одному автору.
type AuthorStat struct {
	AuthorID int64 `json:"author_id"`
	Count    int   `json:"count"`
}

// clickHandler обрабатывает POST /click.
func ClickHandler(tracker *service.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ClickRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		tracker.AddClick(req.AuthorID, req.UserID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// statsHandler обрабатывает GET /stats.
// Параметр author_ids передаётся как query-строка, например: ?author_ids=1,2,3
func StatsHandler(tracker *service.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		idsParam := r.URL.Query().Get("author_ids")
		if idsParam == "" {
			http.Error(w, "Missing author_ids parameter", http.StatusBadRequest)
			return
		}

		parts := strings.Split(idsParam, ",")
		authorIDs := make([]int64, 0, len(parts))
		for _, part := range parts {
			id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err != nil {
				http.Error(w, "Invalid author_id", http.StatusBadRequest)
				return
			}
			authorIDs = append(authorIDs, id)
		}

		stats := tracker.GetYesterdayStats(authorIDs)

		resp := StatsResponse{
			Stats: make([]AuthorStat, 0, len(authorIDs)),
		}
		for _, id := range authorIDs {
			resp.Stats = append(resp.Stats, AuthorStat{
				AuthorID: id,
				Count:    stats[id],
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}
