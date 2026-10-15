package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
)

// mockClient — управляемый мок StorageGatewayClient.
type mockClient struct {
	mu sync.Mutex

	authors    []domain.Author
	authorsErr error

	statsByDate map[string][]domain.AuthorStats
	statsErr    error
	statsFn     func(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
}

func (m *mockClient) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	if m.authorsErr != nil {
		return nil, m.authorsErr
	}
	return m.authors, nil
}

func (m *mockClient) GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	if m.statsFn != nil {
		return m.statsFn(ctx, date)
	}
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.statsByDate == nil {
		return nil, nil
	}
	return m.statsByDate[date.Format("2006-01-02")], nil
}

func TestNewStatsService_DefaultConcurrency(t *testing.T) {
	svc := NewStatsService(&mockClient{}, 30, 0)
	if cap(svc.sem) != 5 {
		t.Errorf("concurrency = %d, want 5", cap(svc.sem))
	}
	svc = NewStatsService(&mockClient{}, 30, -1)
	if cap(svc.sem) != 5 {
		t.Errorf("concurrency = %d, want 5", cap(svc.sem))
	}
}

func TestMaxDays(t *testing.T) {
	svc := NewStatsService(&mockClient{}, 42, 3)
	if svc.MaxDays() != 42 {
		t.Errorf("MaxDays = %d, want 42", svc.MaxDays())
	}
}

func TestGetStats_InvalidDays(t *testing.T) {
	svc := NewStatsService(&mockClient{}, 10, 3)

	cases := []int{0, -1, 11, 100}
	for _, d := range cases {
		_, err := svc.GetStats(context.Background(), d)
		if !errors.Is(err, ErrInvalidDays) {
			t.Errorf("days=%d: got %v, want ErrInvalidDays", d, err)
		}
	}
}

func TestGetStats_Success(t *testing.T) {
	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	d0 := yesterday.Format("2006-01-02")
	d1 := yesterday.AddDate(0, 0, -1).Format("2006-01-02")
	d2 := yesterday.AddDate(0, 0, -2).Format("2006-01-02")

	client := &mockClient{
		authors: []domain.Author{
			{ID: 2, FirstName: "Bob", LastName: "B", Email: "b@b"},
			{ID: 1, FirstName: "Alice", LastName: "A", Email: "a@a"},
		},
		statsByDate: map[string][]domain.AuthorStats{
			d0: {{AuthorID: 1, Count: 10}, {AuthorID: 2, Count: 5}},
			d1: {{AuthorID: 1, Count: 7}}, // у автора 2 ничего нет — должно остаться 0
			// d2 — пусто совсем
		},
	}

	svc := NewStatsService(client, 30, 3)
	res, err := svc.GetStats(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if res.MaxDays != 30 {
		t.Errorf("MaxDays = %d, want 30", res.MaxDays)
	}
	if len(res.Dates) != 3 {
		t.Fatalf("len(Dates) = %d, want 3", len(res.Dates))
	}
	if res.Dates[0] != d0 || res.Dates[1] != d1 || res.Dates[2] != d2 {
		t.Errorf("dates = %v, want [%s %s %s]", res.Dates, d0, d1, d2)
	}

	// Авторы отсортированы по ID.
	if len(res.Authors) != 2 || res.Authors[0].ID != 1 || res.Authors[1].ID != 2 {
		t.Fatalf("authors order = %+v", res.Authors)
	}

	alice := res.Authors[0]
	if got := alice.Clicks; len(got) != 3 || got[0] != 10 || got[1] != 7 || got[2] != 0 {
		t.Errorf("alice clicks = %v, want [10 7 0]", got)
	}

	bob := res.Authors[1]
	if got := bob.Clicks; len(got) != 3 || got[0] != 5 || got[1] != 0 || got[2] != 0 {
		t.Errorf("bob clicks = %v, want [5 0 0]", got)
	}
}

func TestGetStats_AuthorsError(t *testing.T) {
	client := &mockClient{authorsErr: errors.New("boom")}
	svc := NewStatsService(client, 10, 3)

	_, err := svc.GetStats(context.Background(), 3)
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("got %v, want ErrUpstream", err)
	}
}

func TestGetStats_StatsError(t *testing.T) {
	client := &mockClient{
		authors:  []domain.Author{{ID: 1}},
		statsErr: errors.New("boom"),
	}
	svc := NewStatsService(client, 10, 3)

	_, err := svc.GetStats(context.Background(), 3)
	if !errors.Is(err, ErrUpstream) {
		t.Errorf("got %v, want ErrUpstream", err)
	}
}

// Отмена контекста НЕ должна заворачиваться в ErrUpstream — хендлер различает их.
func TestGetStats_ContextCanceled_NotWrapped(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())

	client := &mockClient{
		authors: []domain.Author{{ID: 1}},
		statsFn: func(ctx context.Context, _ time.Time) ([]domain.AuthorStats, error) {
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	svc := NewStatsService(client, 10, 3)
	_, err := svc.GetStats(parentCtx, 3)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrUpstream) {
		t.Errorf("context.Canceled must not be wrapped as ErrUpstream: %v", err)
	}
}

// Семафор должен ограничивать одновременные запросы.
func TestGetStats_RespectsConcurrency(t *testing.T) {
	const concurrency = 3
	const days = 15

	var mu sync.Mutex
	var current, maxSeen int

	client := &mockClient{
		authors: []domain.Author{{ID: 1}},
		statsFn: func(_ context.Context, _ time.Time) ([]domain.AuthorStats, error) {
			mu.Lock()
			current++
			if current > maxSeen {
				maxSeen = current
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			current--
			mu.Unlock()
			return nil, nil
		},
	}

	svc := NewStatsService(client, 100, concurrency)
	if _, err := svc.GetStats(context.Background(), days); err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if maxSeen > concurrency {
		t.Errorf("max concurrent = %d, want <= %d", maxSeen, concurrency)
	}
}
