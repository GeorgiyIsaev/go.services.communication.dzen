package service

import (
	"sync"
	"testing"
	"time"
)

// fixedNow возвращает функцию, всегда возвращающую заданное время.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestDayKey_UsesUTC(t *testing.T) {
	t.Parallel()

	if got := dayKey(time.Date(2024, 1, 1, 23, 59, 59, 0, time.UTC)); got != "2024-01-01" {
		t.Fatalf("got %q, want 2024-01-01", got)
	}
	if got := dayKey(time.Date(2024, 1, 2, 0, 0, 1, 0, time.UTC)); got != "2024-01-02" {
		t.Fatalf("got %q, want 2024-01-02", got)
	}
}

func TestTracker_AddClickAt_UniqueUsers(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	tr.AddClickAt(1, 100, at)
	tr.AddClickAt(1, 100, at) // дубль того же пользователя
	tr.AddClickAt(1, 100, at) // ещё дубль
	tr.AddClickAt(1, 101, at) // новый пользователь

	got := tr.GetYesterdayStats([]int64{1})
	if got[1] != 2 {
		t.Fatalf("got %d, want 2 unique users", got[1])
	}
}

func TestTracker_AddClickAt_ByAuthor(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	tr.AddClickAt(1, 100, at)
	tr.AddClickAt(1, 101, at)
	tr.AddClickAt(2, 100, at) // тот же user, другой author

	got := tr.GetYesterdayStats([]int64{1, 2})
	if got[1] != 2 {
		t.Fatalf("author 1: got %d, want 2", got[1])
	}
	if got[2] != 1 {
		t.Fatalf("author 2: got %d, want 1", got[2])
	}
}

func TestTracker_GetYesterdayStats_SeparateDays(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	yesterday := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	today := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)

	tr.AddClickAt(1, 100, yesterday)
	tr.AddClickAt(1, 101, yesterday)
	tr.AddClickAt(1, 102, today) // не должно попасть в yesterday

	got := tr.GetYesterdayStats([]int64{1})
	if got[1] != 2 {
		t.Fatalf("got %d, want 2", got[1])
	}
}

func TestTracker_AddClickAt_IgnoresOldAndFuture(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	// Позавчера — должно быть проигнорировано.
	tr.AddClickAt(1, 100, time.Date(2024, 1, 8, 10, 0, 0, 0, time.UTC))
	// Завтра — тоже проигнорировано.
	tr.AddClickAt(1, 101, time.Date(2024, 1, 11, 10, 0, 0, 0, time.UTC))
	// Вчера — принято.
	tr.AddClickAt(1, 102, time.Date(2024, 1, 9, 10, 0, 0, 0, time.UTC))

	got := tr.GetYesterdayStats([]int64{1})
	if got[1] != 1 {
		t.Fatalf("got %d, want 1 (только вчерашний клик)", got[1])
	}
}

func TestTracker_GetYesterdayStats_UnknownAuthor(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	got := tr.GetYesterdayStats([]int64{42, 43})
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[42] != 0 || got[43] != 0 {
		t.Fatalf("want zeros for unknown authors, got %#v", got)
	}
}

func TestTracker_CleansOldDays(t *testing.T) {
	t.Parallel()

	tr := NewTracker()

	// День 1: «сегодня» = 1 января, добавляем клик.
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(day1))
	tr.AddClickAt(1, 100, day1)

	tr.mu.RLock()
	_, exists1 := tr.days["2024-01-01"]
	tr.mu.RUnlock()
	if !exists1 {
		t.Fatal("день 2024-01-01 должен существовать")
	}

	// День 3: сдвигаем «сегодня», добавляем клик — старый день должен исчезнуть.
	day3 := time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(day3))
	tr.AddClickAt(1, 200, day3)

	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if _, exists := tr.days["2024-01-01"]; exists {
		t.Fatal("день 2024-01-01 должен был быть удалён")
	}
	if _, exists := tr.days["2024-01-03"]; !exists {
		t.Fatal("день 2024-01-03 должен присутствовать")
	}
}

func TestTracker_AddClick_UsesNowFunc(t *testing.T) {
	t.Parallel()

	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	tr.AddClick(1, 100) // должен попасть в "сегодня" = 2024-01-02

	tr.mu.RLock()
	_, okToday := tr.days["2024-01-02"]
	tr.mu.RUnlock()
	if !okToday {
		t.Fatal("клик должен был попасть в сегодняшний день")
	}

	// Вчерашний срез пуст, т.к. мы добавили клик только за сегодня.
	got := tr.GetYesterdayStats([]int64{1})
	if got[1] != 0 {
		t.Fatalf("got %d, want 0", got[1])
	}
}

func TestTracker_ConcurrentAdd_SameClick(t *testing.T) {
	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))

	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			tr.AddClickAt(1, 100, at)
		}()
	}
	wg.Wait()

	got := tr.GetYesterdayStats([]int64{1})
	if got[1] != 1 {
		t.Fatalf("got %d, want 1 (уникальный пользователь)", got[1])
	}
}

func TestTracker_ConcurrentReadWrite(t *testing.T) {
	tr := NewTracker()
	now := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	tr.SetNowFunc(fixedNow(now))
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := int64(0); i < 1000; i++ {
			tr.AddClickAt(1, i, at)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = tr.GetYesterdayStats([]int64{1, 2, 3})
		}
	}()

	wg.Wait()
}
