package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Sender interface {
	SendNow(ctx context.Context, date time.Time) error
}

type StatsHandler struct {
	sender Sender
	logger *slog.Logger
}

func NewStatsHandler(sender Sender, logger *slog.Logger) *StatsHandler {
	return &StatsHandler{sender: sender, logger: logger}
}

func (h *StatsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /flush", h.Flush)
	mux.HandleFunc("GET /healthz", h.Health)
}

// Flush — ручной триггер отправки. По умолчанию отправляет данные за вчера.
// Опциональный ?date=2006-01-02 позволяет выбрать конкретный день (для отладки).
//
//	curl -X POST http://localhost:8080/flush
//	curl -X POST 'http://localhost:8080/flush?date=2024-01-15'
func (h *StatsHandler) Flush(w http.ResponseWriter, r *http.Request) {
	date := time.Now().UTC().AddDate(0, 0, -1)

	if v := r.URL.Query().Get("date"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date format, expected 2006-01-02")
			return
		}
		date = parsed
	}

	h.logger.InfoContext(r.Context(), "manual flush requested",
		"date", date.Format("2006-01-02"))

	// Асинхронно, чтобы HTTP-ответ не висел до конца всех ретраев.
	go func() {
		if err := h.sender.SendNow(context.Background(), date); err != nil {
			h.logger.Error("manual flush failed", "err", err)
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
		"date":   date.Format("2006-01-02"),
	})
}

func (h *StatsHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
