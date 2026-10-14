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

// StatsService — интерфейс
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

// RegisterRoutes регистрирует маршруты (Go 1.22+ method patterns).
func (h *StatsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /stats", h.GetStats)
	mux.HandleFunc("GET /config", h.GetConfig)
	mux.HandleFunc("GET /healthz", h.Health)
}

// GetConfig — отдаёт фронту лимиты (сейчас только max_days).
// Фронт дёргает его при загрузке и выставляет max в input.
func (h *StatsHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"max_days": h.svc.MaxDays(),
	})
}

// Health — простой healthcheck.
func (h *StatsHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// GetStats обрабатывает GET /stats?days=N.
//
// Ответ 200:
//
//	{"dates": ["2024-01-15", ...], "authors": [...], "max_days": 30}
//
// Ошибки:
//   - 400 — days не положительное число или > max_days
//   - 502 — StorageGateway недоступен или вернул ошибку
//   - 504 — общий таймаут 30 сек
//   - 500 — что-то другое
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	const defaultDays = 5

	days := defaultDays
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

	// Общий таймаут на весь запрос /stats, включая все параллельные вызовы StorageGateway.
	// Клиентский таймаут на один запрос задан в config (STORAGE_GATEWAY_TIMEOUT).
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()
	result, err := h.svc.GetStats(ctx, days)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidDays):
			h.logger.WarnContext(r.Context(), "invalid days", "err", err)
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, context.DeadlineExceeded):
			h.logger.ErrorContext(r.Context(), "request timed out",
				"err", err, "days", days, "duration", time.Since(start))
			writeError(w, http.StatusGatewayTimeout, "upstream timeout")

		case errors.Is(err, service.ErrUpstream):
			// Не раскрываем детали наружу — только общее сообщение.
			// Реальные детали (HTTP status, тело) — в логах выше.
			h.logger.ErrorContext(r.Context(), "upstream error",
				"err", err, "days", days, "duration", time.Since(start))
			writeError(w, http.StatusBadGateway, "storage gateway unavailable")

		default:
			h.logger.ErrorContext(r.Context(), "internal error",
				"err", err, "days", days)
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
