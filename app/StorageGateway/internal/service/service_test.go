package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
)

// mockRepo — ручной мок StatsRepository.
type mockRepo struct {
	upsertStatsFn    func(ctx context.Context, date time.Time, stats []domain.AuthorStats) error
	getStatsByDateFn func(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
	getAuthorsFn     func(ctx context.Context) ([]domain.Author, error)
	getStatsRangeFn  func(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error)
}

func (m *mockRepo) UpsertStats(ctx context.Context, date time.Time, stats []domain.AuthorStats) error {
	if m.upsertStatsFn == nil {
		return nil
	}
	return m.upsertStatsFn(ctx, date, stats)
}

func (m *mockRepo) GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	if m.getStatsByDateFn == nil {
		return nil, nil
	}
	return m.getStatsByDateFn(ctx, date)
}

func (m *mockRepo) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	if m.getAuthorsFn == nil {
		return nil, nil
	}
	return m.getAuthorsFn(ctx)
}

func (m *mockRepo) GetStatsRange(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error) {
	if m.getStatsRangeFn == nil {
		return nil, nil
	}
	return m.getStatsRangeFn(ctx, from, to)
}

// fixedNow — «замороженное» время для детерминированных тестов.
var fixedNow = time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

func newTestService(repo StatsRepository) *StorageService {
	svc := NewStorageService(repo)
	svc.now = func() time.Time { return fixedNow }
	return svc
}

func TestSaveStats(t *testing.T) {
	validDate := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	validStats := []domain.AuthorStats{{AuthorID: 1, Count: 10}}
	repoErr := errors.New("db is down")

	tests := []struct {
		name       string
		stats      []domain.AuthorStats
		date       time.Time
		repoErr    error
		wantErrIs  error // errors.Is
		wantAnyErr bool  // любая ошибка
		wantCall   bool  // репозиторий должен быть вызван
	}{
		{
			name:     "success",
			stats:    validStats,
			date:     validDate,
			wantCall: true,
		},
		{
			name:      "zero date",
			stats:     validStats,
			date:      time.Time{},
			wantErrIs: ErrInvalidDate,
		},
		{
			name:      "empty stats",
			stats:     nil,
			date:      validDate,
			wantErrIs: ErrInvalidStats,
		},
		{
			name:      "author_id is zero",
			stats:     []domain.AuthorStats{{AuthorID: 0, Count: 1}},
			date:      validDate,
			wantErrIs: ErrInvalidStats,
		},
		{
			name:      "author_id is negative",
			stats:     []domain.AuthorStats{{AuthorID: -5, Count: 1}},
			date:      validDate,
			wantErrIs: ErrInvalidStats,
		},
		{
			name:      "count is negative",
			stats:     []domain.AuthorStats{{AuthorID: 1, Count: -1}},
			date:      validDate,
			wantErrIs: ErrInvalidStats,
		},
		{
			name:      "date in future",
			stats:     validStats,
			date:      fixedNow.AddDate(0, 0, 1),
			wantErrIs: ErrDateInFuture,
		},
		{
			name:       "repo error is returned",
			stats:      validStats,
			date:       validDate,
			repoErr:    repoErr,
			wantAnyErr: true,
			wantCall:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			repo := &mockRepo{
				upsertStatsFn: func(ctx context.Context, date time.Time, stats []domain.AuthorStats) error {
					called = true
					return tt.repoErr
				},
			}
			svc := newTestService(repo)

			err := svc.SaveStats(context.Background(), tt.stats, tt.date)

			switch {
			case tt.wantErrIs != nil:
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("want error %v, got %v", tt.wantErrIs, err)
				}
			case tt.wantAnyErr:
				if err == nil {
					t.Fatalf("want error, got nil")
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if called != tt.wantCall {
				t.Fatalf("repo called = %v, want %v", called, tt.wantCall)
			}
		})
	}
}

// SaveStats должен передавать в репозиторий дату, усечённую до суток в UTC.
func TestSaveStats_TruncatesDateToUTC(t *testing.T) {
	input := time.Date(2025, 1, 10, 23, 45, 0, 0, time.UTC)
	want := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	var got time.Time
	repo := &mockRepo{
		upsertStatsFn: func(_ context.Context, date time.Time, _ []domain.AuthorStats) error {
			got = date
			return nil
		},
	}
	svc := newTestService(repo)

	if err := svc.SaveStats(context.Background(),
		[]domain.AuthorStats{{AuthorID: 1, Count: 1}}, input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Equal(want) {
		t.Fatalf("date passed to repo = %v, want %v", got, want)
	}
}

func TestGetDashboardData(t *testing.T) {
	date := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	repoErr := errors.New("boom")
	wantStats := []domain.AuthorStats{{AuthorID: 1, Count: 7}}

	t.Run("success", func(t *testing.T) {
		var gotDate time.Time
		repo := &mockRepo{
			getStatsByDateFn: func(_ context.Context, d time.Time) ([]domain.AuthorStats, error) {
				gotDate = d
				return wantStats, nil
			},
		}
		svc := newTestService(repo)

		got, err := svc.GetDashboardData(context.Background(), date)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != wantStats[0] {
			t.Fatalf("got %v, want %v", got, wantStats)
		}
		if !gotDate.Equal(date) {
			t.Fatalf("date passed to repo = %v, want %v", gotDate, date)
		}
	})

	t.Run("zero date", func(t *testing.T) {
		repo := &mockRepo{}
		svc := newTestService(repo)

		_, err := svc.GetDashboardData(context.Background(), time.Time{})
		if !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("want ErrInvalidDate, got %v", err)
		}
	})

	t.Run("repo error is wrapped", func(t *testing.T) {
		repo := &mockRepo{
			getStatsByDateFn: func(context.Context, time.Time) ([]domain.AuthorStats, error) {
				return nil, repoErr
			},
		}
		svc := newTestService(repo)

		_, err := svc.GetDashboardData(context.Background(), date)
		if err == nil {
			t.Fatal("want error, got nil")
		}
		if !errors.Is(err, repoErr) {
			t.Fatalf("want wrapped %v, got %v", repoErr, err)
		}
	})
}

func TestGetAuthors(t *testing.T) {
	want := []domain.Author{{ID: 1, FirstName: "Ivan", LastName: "Petrov", Email: "i@p.local"}}
	repoErr := errors.New("boom")

	t.Run("success", func(t *testing.T) {
		repo := &mockRepo{
			getAuthorsFn: func(context.Context) ([]domain.Author, error) {
				return want, nil
			},
		}
		svc := newTestService(repo)

		got, err := svc.GetAuthors(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("repo error is wrapped", func(t *testing.T) {
		repo := &mockRepo{
			getAuthorsFn: func(context.Context) ([]domain.Author, error) {
				return nil, repoErr
			},
		}
		svc := newTestService(repo)

		_, err := svc.GetAuthors(context.Background())
		if !errors.Is(err, repoErr) {
			t.Fatalf("want wrapped %v, got %v", repoErr, err)
		}
	})
}

func TestGetStatsRange(t *testing.T) {
	from := time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	want := map[string][]domain.AuthorStats{
		"2025-01-15": {{AuthorID: 1, Count: 10}},
	}
	repoErr := errors.New("boom")

	t.Run("success", func(t *testing.T) {
		var gotFrom, gotTo time.Time
		repo := &mockRepo{
			getStatsRangeFn: func(_ context.Context, f, tl time.Time) (map[string][]domain.AuthorStats, error) {
				gotFrom, gotTo = f, tl
				return want, nil
			},
		}
		svc := newTestService(repo)

		got, err := svc.GetStatsRange(context.Background(), from, to)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || len(got["2025-01-15"]) != 1 || got["2025-01-15"][0].Count != 10 {
			t.Fatalf("got %v, want %v", got, want)
		}
		if !gotFrom.Equal(from) || !gotTo.Equal(to) {
			t.Fatalf("range passed to repo = [%v, %v], want [%v, %v]", gotFrom, gotTo, from, to)
		}
	})

	t.Run("truncates dates to UTC midnight", func(t *testing.T) {
		var gotFrom, gotTo time.Time
		repo := &mockRepo{
			getStatsRangeFn: func(_ context.Context, f, tl time.Time) (map[string][]domain.AuthorStats, error) {
				gotFrom, gotTo = f, tl
				return nil, nil
			},
		}
		svc := newTestService(repo)

		inFrom := time.Date(2025, 1, 5, 13, 30, 0, 0, time.UTC)
		inTo := time.Date(2025, 1, 15, 23, 59, 0, 0, time.UTC)
		if _, err := svc.GetStatsRange(context.Background(), inFrom, inTo); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !gotFrom.Equal(from) || !gotTo.Equal(to) {
			t.Fatalf("truncated range = [%v, %v], want [%v, %v]", gotFrom, gotTo, from, to)
		}
	})

	t.Run("zero date", func(t *testing.T) {
		svc := newTestService(&mockRepo{})

		if _, err := svc.GetStatsRange(context.Background(), time.Time{}, to); !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("want ErrInvalidDate, got %v", err)
		}
		if _, err := svc.GetStatsRange(context.Background(), from, time.Time{}); !errors.Is(err, ErrInvalidDate) {
			t.Fatalf("want ErrInvalidDate, got %v", err)
		}
	})

	t.Run("repo error is wrapped", func(t *testing.T) {
		repo := &mockRepo{
			getStatsRangeFn: func(context.Context, time.Time, time.Time) (map[string][]domain.AuthorStats, error) {
				return nil, repoErr
			},
		}
		svc := newTestService(repo)

		_, err := svc.GetStatsRange(context.Background(), from, to)
		if !errors.Is(err, repoErr) {
			t.Fatalf("want wrapped %v, got %v", repoErr, err)
		}
	})
}
