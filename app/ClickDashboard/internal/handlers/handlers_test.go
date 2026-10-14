package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newTestHandler(t *testing.T) (*StatsHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStatsHandler(db), mock
}

func expectedRange(days int) (start, yesterday string) {
	now := time.Now().UTC()
	y := now.AddDate(0, 0, -1)
	s := y.AddDate(0, 0, -days+1)
	return s.Format("2006-01-02"), y.Format("2006-01-02")
}

func TestGetStats_MethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/stats", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestGetStats_OptionsPreflight(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest(http.MethodOptions, "/stats", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("CORS header = %q, want *", got)
	}
}

func TestGetStats_Defaults(t *testing.T) {
	h, mock := newTestHandler(t)
	start, yesterday := expectedRange(5) // default days=5

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Dates  []string `json:"dates"`
		Limit  int      `json:"limit"`
		Offset int      `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Limit != 100 || body.Offset != 0 {
		t.Errorf("Limit/Offset = %d/%d, want 100/0", body.Limit, body.Offset)
	}
}

func TestGetStats_DaysClampedToMax(t *testing.T) {
	h, mock := newTestHandler(t)
	// days=999 → ожидаем кламп до 30
	start, yesterday := expectedRange(30)

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	req := httptest.NewRequest(http.MethodGet, "/stats?days=999", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGetStats_LimitClampedToMax(t *testing.T) {
	h, mock := newTestHandler(t)
	start, yesterday := expectedRange(5)

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	req := httptest.NewRequest(http.MethodGet, "/stats?limit=100000", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Аргументы пагинации проверяются в сервисных тестах — здесь важно, что нет паники и 200.
}

func TestGetStats_NegativeOffsetIgnored(t *testing.T) {
	h, mock := newTestHandler(t)
	start, yesterday := expectedRange(5)

	// offset=-5 должен откатиться к 0
	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	req := httptest.NewRequest(http.MethodGet, "/stats?offset=-5", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestGetStats_ServiceErrorReturns500(t *testing.T) {
	h, mock := newTestHandler(t)

	mock.ExpectQuery("SELECT DISTINCT date").
		WillReturnError(errors.New("db down"))

	req := httptest.NewRequest(http.MethodGet, "/stats?days=5", nil)
	rec := httptest.NewRecorder()
	h.GetStats(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); body == "" || body == "db down\n" {
		// главное — не светим внутреннюю ошибку
		t.Errorf("body leaks internal error: %q", body)
	}
}
