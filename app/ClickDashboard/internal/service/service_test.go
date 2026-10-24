package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockClient — управляемый мок StorageGatewayClient.
// GetStatsRange отдаёт данные, построенные относительно запрошенного `to`,
// чтобы тесты не зависели от реальной даты.
type mockClient struct {
	authors    []domain.Author
	authorsErr error

	statsByDate func(from, to time.Time) map[string][]domain.AuthorStats
	statsErr    error

	mu      sync.Mutex
	gotFrom time.Time
	gotTo   time.Time
	calls   int
}

func (m *mockClient) GetAuthors(context.Context) ([]domain.Author, error) {
	if m.authorsErr != nil {
		return nil, m.authorsErr
	}
	return m.authors, nil
}

func (m *mockClient) GetStatsRange(_ context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error) {
	m.mu.Lock()
	m.gotFrom, m.gotTo = from, to
	m.calls++
	m.mu.Unlock()

	if m.statsErr != nil {
		return nil, m.statsErr
	}
	if m.statsByDate != nil {
		return m.statsByDate(from, to), nil
	}
	return nil, nil
}

func (m *mockClient) rangeCall() (time.Time, time.Time, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gotFrom, m.gotTo, m.calls
}

func newTestService(client StorageGatewayClient, maxDays int) (*StatsService, *Store) {
	store := NewStore()
	svc := NewStatsService(client, store, testLogger(), maxDays, time.Minute)
	return svc, store
}

func TestMaxDays(t *testing.T) {
	svc := NewStatsService(&mockClient{}, NewStore(), testLogger(), 42, time.Minute)
	if svc.MaxDays() != 42 {
		t.Errorf("MaxDays = %d, want 42", svc.MaxDays())
	}
}

func TestGetStats_InvalidDays(t *testing.T) {
	svc, _ := newTestService(&mockClient{}, 10)

	cases := []int{0, -1, 11, 100}
	for _, d := range cases {
		_, err := svc.GetStats(context.Background(), d)
		if !errors.Is(err, ErrInvalidDays) {
			t.Errorf("days=%d: got %v, want ErrInvalidDays", d, err)
		}
	}
}

// До первого успешного Refresh снапшота нет — сервис отвечает ErrSnapshotNotReady.
func TestGetStats_SnapshotNotReady(t *testing.T) {
	svc, _ := newTestService(&mockClient{}, 30)

	_, err := svc.GetStats(context.Background(), 5)
	if !errors.Is(err, ErrSnapshotNotReady) {
		t.Errorf("got %v, want ErrSnapshotNotReady", err)
	}
}

// Refresh тянет authors + stats за диапазон [today-(maxDays-1) .. today]
// и публикует снапшот; GetStats отдаёт его нарезкой.
func TestRefresh_PublishesSnapshot(t *testing.T) {
	client := &mockClient{
		// Порядок задан специально не по ID — снапшот должен отсортировать.
		authors: []domain.Author{
			{ID: 2, FirstName: "Bob", LastName: "B", Email: "b@b"},
			{ID: 1, FirstName: "Alice", LastName: "A", Email: "a@a"},
		},
		statsByDate: func(_, to time.Time) map[string][]domain.AuthorStats {
			return map[string][]domain.AuthorStats{
				to.Format("2006-01-02"):                   {{AuthorID: 1, Count: 10}, {AuthorID: 2, Count: 5}},
				to.AddDate(0, 0, -1).Format("2006-01-02"): {{AuthorID: 1, Count: 7}},
				// третий день данных не имеет — остаётся 0
			}
		},
	}
	svc, _ := newTestService(client, 30)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Диапазон запрашивается за maxDays суток, включая сегодня, в UTC-полночь.
	gotFrom, gotTo, calls := client.rangeCall()
	if calls != 1 {
		t.Fatalf("GetStatsRange calls = %d, want 1", calls)
	}
	if !gotTo.Equal(gotTo.Truncate(24*time.Hour)) || gotTo.Location() != time.UTC {
		t.Errorf("to = %v, want UTC midnight", gotTo)
	}
	if want := gotTo.AddDate(0, 0, -(30 - 1)); !gotFrom.Equal(want) {
		t.Errorf("from = %v, want %v", gotFrom, want)
	}

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
	d0 := gotTo.Format("2006-01-02")
	d1 := gotTo.AddDate(0, 0, -1).Format("2006-01-02")
	d2 := gotTo.AddDate(0, 0, -2).Format("2006-01-02")
	if res.Dates[0] != d0 || res.Dates[1] != d1 || res.Dates[2] != d2 {
		t.Errorf("dates = %v, want [%s %s %s] (по убыванию)", res.Dates, d0, d1, d2)
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

// Slice(days) отдаёт первые days столбцов снапшота.
func TestGetStats_SliceShorterRange(t *testing.T) {
	client := &mockClient{
		authors: []domain.Author{{ID: 1, FirstName: "Alice"}},
		statsByDate: func(_, to time.Time) map[string][]domain.AuthorStats {
			return map[string][]domain.AuthorStats{
				to.Format("2006-01-02"): {{AuthorID: 1, Count: 3}},
			}
		},
	}
	svc, _ := newTestService(client, 30)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	res, err := svc.GetStats(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if len(res.Dates) != 1 {
		t.Fatalf("len(Dates) = %d, want 1", len(res.Dates))
	}
	if got := res.Authors[0].Clicks; len(got) != 1 || got[0] != 3 {
		t.Errorf("clicks = %v, want [3]", got)
	}
}

// Ошибка клиента при Refresh возвращает err и НЕ публикует снапшот.
func TestRefresh_AuthorsError(t *testing.T) {
	client := &mockClient{authorsErr: errors.New("boom")}
	svc, store := newTestService(client, 10)

	err := svc.Refresh(context.Background())
	if err == nil || !errors.Is(err, client.authorsErr) {
		t.Fatalf("Refresh err = %v, want wrapped boom", err)
	}
	if store.Get() != nil {
		t.Error("snapshot must not be published on error")
	}
	if _, err := svc.GetStats(context.Background(), 3); !errors.Is(err, ErrSnapshotNotReady) {
		t.Errorf("got %v, want ErrSnapshotNotReady", err)
	}
}

func TestRefresh_StatsRangeError(t *testing.T) {
	client := &mockClient{
		authors:  []domain.Author{{ID: 1}},
		statsErr: errors.New("boom"),
	}
	svc, store := newTestService(client, 10)

	err := svc.Refresh(context.Background())
	if err == nil || !errors.Is(err, client.statsErr) {
		t.Fatalf("Refresh err = %v, want wrapped boom", err)
	}
	if store.Get() != nil {
		t.Error("snapshot must not be published on error")
	}
}

// Автор, которого нет в справочнике, но по которому есть статистика,
// получает строку-заглушку — ту же, что раньше создавал ensureAuthors в БД.
func TestRefresh_SynthesizesPlaceholderForUnknownAuthor(t *testing.T) {
	client := &mockClient{
		authors: []domain.Author{
			{ID: 1, FirstName: "Alice", LastName: "A", Email: "a@a"},
		},
		statsByDate: func(_, to time.Time) map[string][]domain.AuthorStats {
			return map[string][]domain.AuthorStats{
				to.Format("2006-01-02"): {{AuthorID: 1, Count: 10}, {AuthorID: 250, Count: 5}},
			}
		},
	}
	svc, _ := newTestService(client, 30)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	res, err := svc.GetStats(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}

	if len(res.Authors) != 2 {
		t.Fatalf("len(Authors) = %d, want 2: %+v", len(res.Authors), res.Authors)
	}

	// id=1 — настоящий автор из справочника.
	real := res.Authors[0]
	if real.ID != 1 || real.FirstName != "Alice" || real.Email != "a@a" {
		t.Errorf("real author = %+v, want id=1 Alice a@a", real)
	}
	if got := real.Clicks; len(got) != 1 || got[0] != 10 {
		t.Errorf("real author clicks = %v, want [10]", got)
	}

	// id=250 — синтезированная заглушка, отсортирована по id.
	ph := res.Authors[1]
	if ph.ID != 250 || ph.FirstName != "unknown" || ph.LastName != "unknown" {
		t.Errorf("placeholder = %+v, want id=250 unknown/unknown", ph)
	}
	if ph.Email != "unknown_250@unknown.local" {
		t.Errorf("placeholder email = %q, want unknown_250@unknown.local", ph.Email)
	}
	if got := ph.Clicks; len(got) != 1 || got[0] != 5 {
		t.Errorf("placeholder clicks = %v, want [5]", got)
	}
}

// Run должен завершиться по отмене ctx, даже если клиент постоянно падает
// (стартовые ретраи не должны подвесить процесс).
func TestRun_StopsOnContextCancel(t *testing.T) {
	client := &mockClient{authorsErr: errors.New("gateway is down")}
	svc, _ := newTestService(client, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем сразу: Refresh упадёт, цикл увидит ctx.Done()

	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// Конкурентное чтение из атомарного снапшота (запускать с -race).
func TestGetStats_ConcurrentReadsAfterRefresh(t *testing.T) {
	client := &mockClient{
		authors: []domain.Author{{ID: 1, FirstName: "Alice"}},
		statsByDate: func(_, to time.Time) map[string][]domain.AuthorStats {
			return map[string][]domain.AuthorStats{
				to.Format("2006-01-02"): {{AuthorID: 1, Count: 42}},
			}
		},
	}
	svc, _ := newTestService(client, 30)

	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.GetStats(context.Background(), 5); err != nil {
				t.Errorf("GetStats: %v", err)
			}
		}()
	}
	wg.Wait()
}
