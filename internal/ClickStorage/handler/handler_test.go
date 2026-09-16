package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRepo struct {
	stats map[int]int
	err   error
}

func (f *fakeRepo) GetStatsForDate(_ context.Context, _ time.Time) (map[int]int, error) {
	return f.stats, f.err
}

func TestGetStatsHandler_OK(t *testing.T) {
	repo := &fakeRepo{stats: map[int]int{1: 5, 2: 7}}
	h := New(repo)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	h.GetStatsHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want Content-Type application/json, got %q", ct)
	}

	var resp StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rec.Body.String())
	}
	if len(resp.Stats) != 2 {
		t.Fatalf("want 2 stats, got %d", len(resp.Stats))
	}

	byID := make(map[int]int, len(resp.Stats))
	for _, s := range resp.Stats {
		byID[s.AuthorID] = s.Count
	}
	if byID[1] != 5 || byID[2] != 7 {
		t.Fatalf("wrong stats: %v", byID)
	}
}

func TestGetStatsHandler_EmptyReturnsEmptyArrayNotNull(t *testing.T) {
	repo := &fakeRepo{stats: map[int]int{}}
	h := New(repo)

	rec := httptest.NewRecorder()
	h.GetStatsHandler(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	// Важно: если бы resp.Stats не инициализировали через make,
	// было бы "stats":null — а это ломает клиентов, ожидающих массив.
	if !strings.Contains(rec.Body.String(), `"stats":[]`) {
		t.Fatalf("expected stats:[], got body=%s", rec.Body.String())
	}
}

func TestGetStatsHandler_RepoErrorReturns500(t *testing.T) {
	repo := &fakeRepo{err: errors.New("db down")}
	h := New(repo)

	rec := httptest.NewRecorder()
	h.GetStatsHandler(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
}

func TestGetStatsHandler_UsesContextFromRequest(t *testing.T) {
	// Убеждаемся, что хендлер прокидывает ctx из запроса в репозиторий,
	// а не context.Background(). Это важно для graceful shutdown:
	// если клиент отвалится, запрос к БД должен отмениться.
	repo := &ctxCapturingRepo{}
	h := New(repo)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stats", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.GetStatsHandler(rec, req)

	if repo.capturedCtx != ctx {
		t.Fatal("handler must pass request context to repository")
	}
}

type ctxCapturingRepo struct {
	capturedCtx context.Context
}

func (c *ctxCapturingRepo) GetStatsForDate(ctx context.Context, _ time.Time) (map[int]int, error) {
	c.capturedCtx = ctx
	return map[int]int{}, nil
}
