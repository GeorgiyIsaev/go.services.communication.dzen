package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
	"go.services.communication.dzen/app/ClickDashboard/internal/service"
)

type StatsService interface {
	GetStats(ctx context.Context, days int) (*domain.StatsMatrix, error)
	MaxDays() int
}

type StatsHandler struct {
	svc    StatsService
	logger *slog.Logger
}

func NewStatsHandler(svc StatsService, logger *slog.Logger) *StatsHandler {
	return &StatsHandler{svc: svc, logger: logger}
}

func (h *StatsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /stats", h.GetStats)
	mux.HandleFunc("GET /config", h.GetConfig)
	mux.HandleFunc("GET /healthz", h.Health)
}

func (h *StatsHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"max_days": h.svc.MaxDays(),
	})
}

func (h *StatsHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	days := min(5, h.svc.MaxDays())
	if days < 1 {
		days = 1
	}

	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			h.logger.WarnContext(r.Context(), "invalid days param", "days", v)
			writeError(w, http.StatusBadRequest, "query param 'days' must be a positive integer")
			return
		}
		if n > h.svc.MaxDays() {
			h.logger.WarnContext(r.Context(), "days exceeds max",
				"days", n, "max_days", h.svc.MaxDays())
			writeError(w, http.StatusBadRequest,
				"days exceeds maximum allowed ("+strconv.Itoa(h.svc.MaxDays())+")")
			return
		}
		days = n
	}

	start := time.Now()
	result, err := h.svc.GetStats(r.Context(), days)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidDays):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrSnapshotNotReady):
			h.logger.WarnContext(r.Context(), "snapshot not ready", "days", days)
			writeError(w, http.StatusServiceUnavailable,
				"data is loading, please retry in a few seconds")
		default:
			h.logger.ErrorContext(r.Context(), "internal error", "err", err, "days", days)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.logger.InfoContext(r.Context(), "stats served",
		"days", days,
		"authors", len(result.Authors),
		"duration", time.Since(start))

	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
