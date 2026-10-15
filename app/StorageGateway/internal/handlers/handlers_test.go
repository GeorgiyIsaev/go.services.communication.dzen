package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
	"go.services.communication.dzen/app/StorageGateway/internal/service"
)

// mockSvc — ручной мок StorageService.
type mockSvc struct {
	saveStatsFn        func(ctx context.Context, stats []domain.AuthorStats, date time.Time) error
	getDashboardDataFn func(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
	getAuthorsFn       func(ctx context.Context) ([]domain.Author, error)
}

func (m *mockSvc) SaveStats(ctx context.Context, stats []domain.AuthorStats, date time.Time) error {
	if m.saveStatsFn == nil {
		return nil
	}
	return m.saveStatsFn(ctx, stats, date)
}

func (m *mockSvc) GetDashboardData(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	if m.getDashboardDataFn == nil {
		return nil, nil
	}
	return m.getDashboardDataFn(ctx, date)
}

func (m *mockSvc) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	if m.getAuthorsFn == nil {
		return nil, nil
	}
	return m.getAuthorsFn(ctx)
}

func newTestHandler(svc StorageService) *StorageHandler {
	return NewStorageHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func doRequest(t *testing.T, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rr := httptest.NewRecorder()
	h(rr, r)
	return rr
}

// --- SaveStats ---

func TestHandler_SaveStats(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		svcErr     error
		wantStatus int
	}{
		{
			name:       "success",
			body:       `{"date":"2025-01-10","stats":[{"author_id":1,"count":5}]}`,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "invalid json",
			body:       `{not json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"date":"2025-01-10","stats":[],"extra":1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing date",
			body:       `{"stats":[{"author_id":1,"count":5}]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "bad date format",
			body:       `{"date":"10-01-2025","stats":[{"author_id":1,"count":5}]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service validation error",
			body:       `{"date":"2025-01-10","stats":[{"author_id":1,"count":5}]}`,
			svcErr:     service.ErrInvalidStats,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service invalid date",
			body:       `{"date":"2025-01-10","stats":[{"author_id":1,"count":5}]}`,
			svcErr:     service.ErrInvalidDate,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service date in future",
			body:       `{"date":"2025-01-10","stats":[{"author_id":1,"count":5}]}`,
			svcErr:     service.ErrDateInFuture,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service internal error",
			body:       `{"date":"2025-01-10","stats":[{"author_id":1,"count":5}]}`,
			svcErr:     errors.New("db down"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockSvc{
				saveStatsFn: func(context.Context, []domain.AuthorStats, time.Time) error {
					return tt.svcErr
				},
			}
			h := newTestHandler(svc)

			rr := doRequest(t, h.SaveStats, http.MethodPost, "/stats", tt.body)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%q)", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

// Проверяем, что в сервис попадают корректные дата и статистика.
func TestHandler_SaveStats_PassesParsedValues(t *testing.T) {
	var (
		gotDate  time.Time
		gotStats []domain.AuthorStats
	)
	svc := &mockSvc{
		saveStatsFn: func(_ context.Context, stats []domain.AuthorStats, date time.Time) error {
			gotDate = date
			gotStats = stats
			return nil
		},
	}
	h := newTestHandler(svc)

	body := `{"date":"2025-01-10","stats":[{"author_id":42,"count":7}]}`
	rr := doRequest(t, h.SaveStats, http.MethodPost, "/stats", body)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}

	wantDate := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	if !gotDate.Equal(wantDate) {
		t.Fatalf("date = %v, want %v", gotDate, wantDate)
	}
	if len(gotStats) != 1 || gotStats[0].AuthorID != 42 || gotStats[0].Count != 7 {
		t.Fatalf("stats = %+v", gotStats)
	}
}

// --- GetDashboardData ---

func TestHandler_GetDashboardData(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		svcErr     error
		wantStatus int
	}{
		{
			name:       "success",
			target:     "/dashboard?date=2025-01-10",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing date",
			target:     "/dashboard",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid date format",
			target:     "/dashboard?date=10-01-2025",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service invalid date",
			target:     "/dashboard?date=2025-01-10",
			svcErr:     service.ErrInvalidDate,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service internal error",
			target:     "/dashboard?date=2025-01-10",
			svcErr:     errors.New("boom"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockSvc{
				getDashboardDataFn: func(context.Context, time.Time) ([]domain.AuthorStats, error) {
					if tt.svcErr != nil {
						return nil, tt.svcErr
					}
					return []domain.AuthorStats{{AuthorID: 1, Count: 10}}, nil
				},
			}
			h := newTestHandler(svc)

			rr := doRequest(t, h.GetDashboardData, http.MethodGet, tt.target, "")
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%q)", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandler_GetDashboardData_ResponseShape(t *testing.T) {
	svc := &mockSvc{
		getDashboardDataFn: func(context.Context, time.Time) ([]domain.AuthorStats, error) {
			return []domain.AuthorStats{{AuthorID: 1, Count: 10}}, nil
		},
	}
	h := newTestHandler(svc)

	rr := doRequest(t, h.GetDashboardData, http.MethodGet, "/dashboard?date=2025-01-10", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}

	var resp GetDashboardDataResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantDate := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	if !resp.Date.Equal(wantDate) {
		t.Fatalf("date = %v, want %v", resp.Date, wantDate)
	}
	if len(resp.Stats) != 1 || resp.Stats[0].Count != 10 {
		t.Fatalf("stats = %+v", resp.Stats)
	}
}

// --- GetAuthors ---

func TestHandler_GetAuthors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockSvc{
			getAuthorsFn: func(context.Context) ([]domain.Author, error) {
				return []domain.Author{
					{ID: 1, FirstName: "Ivan", LastName: "Petrov", Email: "i@p.local"},
				}, nil
			},
		}
		h := newTestHandler(svc)

		rr := doRequest(t, h.GetAuthors, http.MethodGet, "/authors", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		var resp GetAuthorsResponse
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Authors) != 1 || resp.Authors[0].ID != 1 {
			t.Fatalf("authors = %+v", resp.Authors)
		}
	})

	t.Run("service error", func(t *testing.T) {
		svc := &mockSvc{
			getAuthorsFn: func(context.Context) ([]domain.Author, error) {
				return nil, errors.New("boom")
			},
		}
		h := newTestHandler(svc)

		rr := doRequest(t, h.GetAuthors, http.MethodGet, "/authors", "")
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rr.Code)
		}
	})
}
