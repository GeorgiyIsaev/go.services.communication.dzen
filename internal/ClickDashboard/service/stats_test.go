package service

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newTestService(t *testing.T) (*StatsService, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStatsService(db), mock
}

// expectedRange повторяет расчёт из сервиса, чтобы совпали аргументы.
func expectedRange(days int) (start, yesterday string) {
	now := time.Now().UTC()
	y := now.AddDate(0, 0, -1)
	s := y.AddDate(0, 0, -days+1)
	return s.Format("2006-01-02"), y.Format("2006-01-02")
}

// utcMidnight возвращает date без времени в UTC.
func utcMidnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func TestGetStats_NoDates_ReturnsEmptyAuthorsAndTotal(t *testing.T) {
	svc, mock := newTestService(t)
	start, yesterday := expectedRange(5)

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	resp, err := svc.GetStats(5, 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Dates) != 0 {
		t.Errorf("Dates = %v, want empty", resp.Dates)
	}
	if resp.Authors == nil || len(resp.Authors) != 0 {
		t.Errorf("Authors = %v, want empty non-nil slice", resp.Authors)
	}
	if resp.Total != 42 {
		t.Errorf("Total = %d, want 42", resp.Total)
	}
	if resp.Limit != 100 || resp.Offset != 0 {
		t.Errorf("Limit/Offset = %d/%d, want 100/0", resp.Limit, resp.Offset)
	}
}

func TestGetStats_HappyPath(t *testing.T) {
	svc, mock := newTestService(t)
	start, yesterday := expectedRange(5)

	y := utcMidnight(time.Now().UTC().AddDate(0, 0, -1))
	dayBefore := y.AddDate(0, 0, -1)

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}).
			AddRow(y).
			AddRow(dayBefore))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectQuery("LEFT JOIN stats").
		WithArgs(start, yesterday, 100, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "first_name", "last_name", "date", "clicks"}).
			AddRow(1, "a@b.c", "Ivan", "Ivanov", y, 7).
			AddRow(1, "a@b.c", "Ivan", "Ivanov", dayBefore, 3))

	resp, err := svc.GetStats(5, 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantDates := []string{y.Format("2006-01-02"), dayBefore.Format("2006-01-02")}
	if len(resp.Dates) != 2 || resp.Dates[0] != wantDates[0] || resp.Dates[1] != wantDates[1] {
		t.Fatalf("Dates = %v, want %v", resp.Dates, wantDates)
	}
	if len(resp.Authors) != 1 {
		t.Fatalf("len(Authors) = %d, want 1", len(resp.Authors))
	}
	got := resp.Authors[0]
	if got.ID != 1 || got.Name == "" {
		t.Errorf("author = %+v", got)
	}
	wantClicks := []int{7, 3}
	for i, c := range got.Clicks {
		if c != wantClicks[i] {
			t.Errorf("Clicks[%d] = %d, want %d", i, c, wantClicks[i])
		}
	}
}

func TestGetStats_AuthorWithoutClicks_NullsHandled(t *testing.T) {
	svc, mock := newTestService(t)
	start, yesterday := expectedRange(5)
	y := utcMidnight(time.Now().UTC().AddDate(0, 0, -1))

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}).AddRow(y))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// LEFT JOIN: у автора нет записей → NULL/NULL
	mock.ExpectQuery("LEFT JOIN stats").
		WithArgs(start, yesterday, 100, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "first_name", "last_name", "date", "clicks"}).
			AddRow(1, "a@b.c", "Ivan", "Ivanov", nil, nil))

	resp, err := svc.GetStats(5, 100, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Authors) != 1 {
		t.Fatalf("len(Authors) = %d, want 1", len(resp.Authors))
	}
	if got := resp.Authors[0].Clicks; len(got) != 1 || got[0] != 0 {
		t.Errorf("Clicks = %v, want [0]", got)
	}
}

func TestGetStats_PaginationArgsArePassed(t *testing.T) {
	svc, mock := newTestService(t)
	start, yesterday := expectedRange(5)
	y := utcMidnight(time.Now().UTC().AddDate(0, 0, -1))

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnRows(sqlmock.NewRows([]string{"date"}).AddRow(y))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM authors")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(500))

	// Ждём limit=25, offset=50
	mock.ExpectQuery("LEFT JOIN stats").
		WithArgs(start, yesterday, 25, 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "first_name", "last_name", "date", "clicks"}))

	resp, err := svc.GetStats(5, 25, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Limit != 25 || resp.Offset != 50 {
		t.Errorf("Limit/Offset = %d/%d, want 25/50", resp.Limit, resp.Offset)
	}
}

func TestGetStats_FirstQueryError(t *testing.T) {
	svc, mock := newTestService(t)
	start, yesterday := expectedRange(5)

	mock.ExpectQuery("SELECT DISTINCT date").
		WithArgs(start, yesterday).
		WillReturnError(errors.New("boom"))

	if _, err := svc.GetStats(5, 100, 0); err == nil {
		t.Fatal("expected error, got nil")
	}
}
