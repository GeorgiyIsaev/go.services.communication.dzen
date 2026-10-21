package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
)

// Sender — интерфейс отправки в StorageGateway.
type Sender interface {
	SendNow(ctx context.Context, date time.Time) error
}

// TrackerReader — интерфейс чтения накопленной статистики.
// Реализуется service.Tracker.
type TrackerReader interface {
	GetByDate(date time.Time) []domain.AuthorStat
}

type StatsHandler struct {
	sender  Sender
	tracker TrackerReader
	logger  *slog.Logger
}

func NewStatsHandler(sender Sender, tracker TrackerReader, logger *slog.Logger) *StatsHandler {
	return &StatsHandler{sender: sender, tracker: tracker, logger: logger}
}

func (h *StatsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /flush/yesterday", h.FlushYesterday)
	mux.HandleFunc("POST /flush/today", h.FlushToday)
	mux.HandleFunc("GET /stats/yesterday", h.StatsYesterday)
	mux.HandleFunc("GET /stats/today", h.StatsToday)
	mux.HandleFunc("GET /healthz", h.Health)
}

// utcYesterday возвращает дату «вчера» по UTC (полночь).
func utcYesterday() time.Time {
	return time.Now().UTC().AddDate(0, 0, -1)
}

// utcToday возвращает дату «сегодня» по UTC (полночь).
func utcToday() time.Time {
	return time.Now().UTC()
}

// --- Flush ---

// FlushYesterday отправляет в StorageGateway накопленные данные за вчера (UTC).
// Синхронный: 200 = отправлено, 500 = ошибка. Используется для ручной отладки.
func (h *StatsHandler) FlushYesterday(w http.ResponseWriter, r *http.Request) {
	h.flushDate(w, r, utcYesterday())
}

// FlushToday — то же для сегодня (UTC). Для отладки: данные за сегодня
// неполные, но полезно увидеть, что Tracker их видит.
func (h *StatsHandler) FlushToday(w http.ResponseWriter, r *http.Request) {
	h.flushDate(w, r, utcToday())
}

func (h *StatsHandler) flushDate(w http.ResponseWriter, r *http.Request, date time.Time) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	h.logger.InfoContext(r.Context(), "manual flush requested",
		"date", date.Format("2006-01-02"))

	if err := h.sender.SendNow(ctx, date); err != nil {
		h.logger.ErrorContext(r.Context(), "manual flush failed",
			"date", date.Format("2006-01-02"), "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.InfoContext(r.Context(), "manual flush done",
		"date", date.Format("2006-01-02"))

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "sent",
		"date":   date.Format("2006-01-02"),
	})
}

// --- Stats ---

// StatsYesterday возвращает накопленные пары author_id → count за вчера (UTC).
// Ничего не отправляет и не удаляет — read-only срез Tracker.
func (h *StatsHandler) StatsYesterday(w http.ResponseWriter, r *http.Request) {
	h.statsFor(w, r, utcYesterday())
}

// StatsToday — то же для сегодня (UTC).
func (h *StatsHandler) StatsToday(w http.ResponseWriter, r *http.Request) {
	h.statsFor(w, r, utcToday())
}

func (h *StatsHandler) statsFor(w http.ResponseWriter, r *http.Request, date time.Time) {
	stats := h.tracker.GetByDate(date)
	// Tracker.GetByDate отдаёт в порядке map — сортируем для стабильного ответа.
	sort.Slice(stats, func(i, j int) bool { return stats[i].AuthorID < stats[j].AuthorID })

	var totalClicks int64
	for _, s := range stats {
		totalClicks += s.Count
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"date":          date.Format("2006-01-02"),
		"authors_total": len(stats),
		"clicks_total":  totalClicks,
		"stats":         stats,
	})
}

// --- Health ---

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
