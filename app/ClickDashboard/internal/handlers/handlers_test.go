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

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
	"go.services.communication.dzen/app/ClickDashboard/internal/service"
)

type mockStatsService struct {
	matrix  *domain.StatsMatrix
	err     error
	maxDays int

	gotDays int
}

func (m *mockStatsService) GetStats(_ context.Context, days int) (*domain.StatsMatrix, error) {
	m.gotDays = days
	if m.err != nil {
		return nil, m.err
	}
	return m.matrix, nil
}

func (m *mockStatsService) MaxDays() int {
	if m.maxDays == 0 {
		return 30
	}
	return m.maxDays
}

// newTestMux собирает роутер так же, как это делает main.
func newTestMux(svc StatsService) *http.ServeMux {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewStatsHandler(svc, logger)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func doRequest(mux http.Handler, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHealth(t *testing.T) {
	mux := newTestMux(&mockStatsService{})
	rec := doRequest(mux, http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rec.Body.String())
	}
}

func TestGetConfig(t *testing.T) {
	mux := newTestMux(&mockStatsService{maxDays: 42})
	rec := doRequest(mux, http.MethodGet, "/config")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, _ := body["max_days"].(float64); int(v) != 42 {
		t.Errorf("max_days = %v, want 42", body["max_days"])
	}
}

func TestGetStats_DefaultDays(t *testing.T) {
	svc := &mockStatsService{matrix: &domain.StatsMatrix{MaxDays: 30}}
	mux := newTestMux(svc)

	rec := doRequest(mux, http.MethodGet, "/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotDays != 5 {
		t.Errorf("service got days = %d, want 5", svc.gotDays)
	}
}

func TestGetStats_InvalidParam(t *testing.T) {
	mux := newTestMux(&mockStatsService{maxDays: 30})

	cases := []string{"abc", "0", "-1", "1.5"}
	for _, v := range cases {
		rec := doRequest(mux, http.MethodGet, "/stats?days="+v)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("days=%s: status = %d, want 400, body=%s", v, rec.Code, rec.Body.String())
		}
	}
}

func TestGetStats_ExceedsMax(t *testing.T) {
	mux := newTestMux(&mockStatsService{maxDays: 10})
	rec := doRequest(mux, http.MethodGet, "/stats?days=11")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "maximum") {
		t.Errorf("body = %q, want mention of maximum", rec.Body.String())
	}
}

func TestGetStats_Success(t *testing.T) {
	svc := &mockStatsService{
		matrix: &domain.StatsMatrix{
			Dates:   []string{"2024-01-15"},
			Authors: []domain.AuthorRow{{ID: 1, FirstName: "Alice", Clicks: []int64{3}}},
			MaxDays: 30,
		},
	}
	mux := newTestMux(svc)

	rec := doRequest(mux, http.MethodGet, "/stats?days=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotDays != 1 {
		t.Errorf("service got days = %d, want 1", svc.gotDays)
	}

	var got domain.StatsMatrix
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Authors) != 1 || got.Authors[0].ID != 1 {
		t.Errorf("unexpected matrix: %+v", got)
	}
}

func TestGetStats_ErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		svcErr     error
		wantStatus int
	}{
		{"invalid days", service.ErrInvalidDays, http.StatusBadRequest},
		{"upstream", service.ErrUpstream, http.StatusBadGateway},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"unknown", errors.New("something"), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := newTestMux(&mockStatsService{err: tc.svcErr})
			rec := doRequest(mux, http.MethodGet, "/stats?days=1")
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
		})
	}
}

//TODO проблема если в настройках меньше 5 дней упадем
//func TestGetStats_DefaultDaysRespectsMax(t *testing.T) {
//	svc := &mockStatsService{
//		matrix:  &domain.StatsMatrix{},
//		maxDays: 3,
//	}
//	mux := newTestMux(svc)
//
//	rec := doRequest(mux, http.MethodGet, "/stats")
//	if rec.Code != http.StatusOK {
//		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
//	}
//	if svc.gotDays != 3 {
//		t.Errorf("service got days = %d, want 3 (clamped to max)", svc.gotDays)
//	}
//}
