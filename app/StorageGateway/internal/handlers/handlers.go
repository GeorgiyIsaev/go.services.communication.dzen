package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
	"go.services.communication.dzen/app/StorageGateway/internal/service"
)

// StorageService - интерфейс сервиса
type StorageService interface {
	SaveStats(ctx context.Context, stats []domain.AuthorStats, date time.Time) error
	GetDashboardData(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
	GetAuthors(ctx context.Context) ([]domain.Author, error)
	GetStatsRange(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error)
}

type StorageHandler struct {
	svc    StorageService
	logger *slog.Logger
}

func NewStorageHandler(svc StorageService, logger *slog.Logger) *StorageHandler {
	return &StorageHandler{svc: svc, logger: logger}
}

type SaveStatsRequest struct {
	Date  string               `json:"date"`
	Stats []domain.AuthorStats `json:"stats"`
}

type GetDashboardDataResponse struct {
	Date  time.Time            `json:"date"`
	Stats []domain.AuthorStats `json:"stats"`
}

type GetAuthorsResponse struct {
	Authors []domain.Author `json:"authors"`
}

func (h *StorageHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /stats", h.SaveStats)
	mux.HandleFunc("GET /dashboard", h.GetDashboardData)
	mux.HandleFunc("GET /authors", h.GetAuthors)
}

// POST /stats — принимает статистику от ClickCounter.
func (h *StorageHandler) SaveStats(w http.ResponseWriter, r *http.Request) {
	var req SaveStatsRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.logger.WarnContext(r.Context(), "save stats: invalid request body", "err", err)
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Date == "" {
		h.logger.WarnContext(r.Context(), "save stats: missing date")
		writeError(w, http.StatusBadRequest, "field 'date' is required (format: 2006-01-02)")
		return
	}
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		h.logger.WarnContext(r.Context(), "save stats: invalid date format", "date", req.Date)
		writeError(w, http.StatusBadRequest, "invalid 'date' format, expected 2006-01-02")
		return
	}

	if err := h.svc.SaveStats(r.Context(), req.Stats, date); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidStats),
			errors.Is(err, service.ErrInvalidDate),
			errors.Is(err, service.ErrDateInFuture):
			h.logger.WarnContext(r.Context(), "save stats: validation failed", "err", err)
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			h.logger.ErrorContext(r.Context(), "save stats: internal error",
				"err", err, "date", req.Date, "authors", len(req.Stats))
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.logger.InfoContext(r.Context(), "stats saved",
		"date", req.Date, "authors", len(req.Stats))
	w.WriteHeader(http.StatusNoContent)
}

// GET /dashboard?date=2006-01-02 — отдаёт данные для ClickDashboard.
func (h *StorageHandler) GetDashboardData(w http.ResponseWriter, r *http.Request) {
	dateParam := r.URL.Query().Get("date")
	if dateParam == "" {
		h.logger.WarnContext(r.Context(), "dashboard: missing date query param")
		writeError(w, http.StatusBadRequest, "query param 'date' is required (format: 2006-01-02)")
		return
	}
	date, err := time.Parse("2006-01-02", dateParam)
	if err != nil {
		h.logger.WarnContext(r.Context(), "dashboard: invalid date format", "date", dateParam)
		writeError(w, http.StatusBadRequest, "invalid 'date' format, expected 2006-01-02")
		return
	}

	stats, err := h.svc.GetDashboardData(r.Context(), date)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidDate):
			h.logger.WarnContext(r.Context(), "dashboard: invalid date", "err", err)
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			h.logger.ErrorContext(r.Context(), "dashboard: internal error",
				"err", err, "date", dateParam)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.logger.InfoContext(r.Context(), "dashboard served",
		"date", dateParam, "authors", len(stats))

	writeJSON(w, http.StatusOK, GetDashboardDataResponse{
		Date:  date,
		Stats: stats,
	})
}

// GET /authors — отдаёт список всех авторов (id, имя, фамилия, email).
func (h *StorageHandler) GetAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := h.svc.GetAuthors(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "authors: internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.logger.InfoContext(r.Context(), "authors served", "count", len(authors))

	writeJSON(w, http.StatusOK, GetAuthorsResponse{Authors: authors})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
