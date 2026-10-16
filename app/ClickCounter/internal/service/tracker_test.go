package service

import (
	"sync"
	"testing"
	"time"
)

// fixedTracker — Tracker с фиксированным nowFunc для детерминированных тестов.
func fixedTracker(now time.Time) *Tracker {
	tr := NewTracker()
	tr.SetNowFunc(func() time.Time { return now })
	return tr
}

// --- Бизнес-логика: уникальные пользователи ---

func TestTracker_DeduplicatesUniqueUsers(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	// Один user_id кликает 5 раз в течение дня — считаем как 1.
	for i := 0; i < 5; i++ {
		tr.AddClickAt(1, 100, now.Add(-time.Duration(i)*time.Minute))
	}
	// Второй уникальный пользователь.
	tr.AddClickAt(1, 200, now)

	stats := tr.GetByDate(now)
	if len(stats) != 1 {
		t.Fatalf("expected 1 author, got %d: %+v", len(stats), stats)
	}
	if got := stats[0]; got.AuthorID != 1 || got.Count != 2 {
		t.Errorf("expected {AuthorID:1, Count:2}, got %+v", got)
	}
}

func TestTracker_SameUserCountedForEachAuthor(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	// Один user_id у двух разных авторов — считается у каждого.
	tr.AddClickAt(1, 100, now)
	tr.AddClickAt(2, 100, now)

	stats := tr.GetByDate(now)
	if len(stats) != 2 {
		t.Fatalf("expected 2 authors, got %d: %+v", len(stats), stats)
	}
	byAuthor := make(map[int64]int64, len(stats))
	for _, s := range stats {
		byAuthor[s.AuthorID] = s.Count
	}
	if byAuthor[1] != 1 || byAuthor[2] != 1 {
		t.Errorf("expected both authors count=1, got %+v", byAuthor)
	}
}

// --- Границы дня ---

func TestTracker_DayBoundaries(t *testing.T) {
	t.Parallel()

	// now = 15 июня полдень → 14 июня это "вчера" (принимается).
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	lateDay1 := time.Date(2024, 6, 14, 23, 59, 59, 0, time.UTC)
	earlyDay2 := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	noonDay2 := now

	tr.AddClickAt(1, 100, lateDay1)
	tr.AddClickAt(1, 200, earlyDay2)
	tr.AddClickAt(1, 300, noonDay2)

	day1Stats := tr.GetByDate(lateDay1)
	day2Stats := tr.GetByDate(earlyDay2)

	if len(day1Stats) != 1 || day1Stats[0].Count != 1 {
		t.Errorf("day1: expected 1 author with count=1, got %+v", day1Stats)
	}
	if len(day2Stats) != 1 || day2Stats[0].Count != 2 {
		t.Errorf("day2: expected 1 author with count=2, got %+v", day2Stats)
	}
}

func TestTracker_AcceptsYesterday(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	yesterday := now.AddDate(0, 0, -1)
	tr := fixedTracker(now)

	tr.AddClickAt(1, 100, yesterday)

	stats := tr.GetByDate(yesterday)
	if len(stats) != 1 || stats[0].Count != 1 {
		t.Errorf("expected yesterday click to be recorded, got %+v", stats)
	}
}

func TestTracker_RejectsOldMessages(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	tooOld := now.AddDate(0, 0, -2) // позавчера
	tr.AddClickAt(1, 100, tooOld)

	if stats := tr.GetByDate(tooOld); len(stats) != 0 {
		t.Errorf("expected old message to be ignored, got %+v", stats)
	}
}

func TestTracker_RejectsFutureMessages(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	future := now.AddDate(0, 0, 1)
	tr.AddClickAt(1, 100, future)

	if stats := tr.GetByDate(future); len(stats) != 0 {
		t.Errorf("expected future message to be ignored, got %+v", stats)
	}
}

// --- Retention / cleanup ---

func TestTracker_CleanupRemovesDaysBeforeYesterday(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(day1)
	tr.AddClickAt(1, 100, day1)

	// Прыгаем на 2 дня вперёд: day1 становится "позавчера".
	day3 := day1.AddDate(0, 0, 2)
	tr.SetNowFunc(func() time.Time { return day3 })
	tr.AddClickAt(1, 100, day3) // триггерит cleanup

	if stats := tr.GetByDate(day1); len(stats) != 0 {
		t.Errorf("expected day1 to be cleaned up, got %+v", stats)
	}
}

func TestTracker_CleanupKeepsTodayAndYesterday(t *testing.T) {
	t.Parallel()

	day1 := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(day1)
	tr.AddClickAt(1, 100, day1)

	day2 := day1.AddDate(0, 0, 1) // станет "вчера" для day3
	day3 := day2.AddDate(0, 0, 1) // "сегодня"
	tr.SetNowFunc(func() time.Time { return day3 })
	tr.AddClickAt(2, 200, day2) // вчера
	tr.AddClickAt(3, 300, day3) // сегодня

	// day1 должен быть удалён, day2 и day3 — остаться.
	if stats := tr.GetByDate(day1); len(stats) != 0 {
		t.Errorf("expected day1 cleaned up, got %+v", stats)
	}
	if stats := tr.GetByDate(day2); len(stats) != 1 || stats[0].AuthorID != 2 {
		t.Errorf("expected day2 kept with author=2, got %+v", stats)
	}
	if stats := tr.GetByDate(day3); len(stats) != 1 || stats[0].AuthorID != 3 {
		t.Errorf("expected day3 kept with author=3, got %+v", stats)
	}
}

// --- GetByDate ---

func TestTracker_GetByDateEmpty(t *testing.T) {
	t.Parallel()

	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	stats := tr.GetByDate(now)
	if stats == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(stats) != 0 {
		t.Errorf("expected empty slice, got %+v", stats)
	}
}

func TestTracker_GetByDateOnlyReturnsSpecificDay(t *testing.T) {
	t.Parallel()

	day2 := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	day1 := day2.AddDate(0, 0, -1)
	tr := fixedTracker(day2)

	tr.AddClickAt(1, 100, day1) // вчера
	tr.AddClickAt(2, 200, day2) // сегодня

	statsDay1 := tr.GetByDate(day1)
	statsDay2 := tr.GetByDate(day2)

	if len(statsDay1) != 1 || statsDay1[0].AuthorID != 1 {
		t.Errorf("day1: expected only author 1, got %+v", statsDay1)
	}
	if len(statsDay2) != 1 || statsDay2[0].AuthorID != 2 {
		t.Errorf("day2: expected only author 2, got %+v", statsDay2)
	}
}

// --- Smoke-тест AddClick (использует реальное время) ---

func TestTracker_AddClickSmoke(t *testing.T) {
	// Не параллелим: завязан на реальное time.Now().
	tr := NewTracker()
	tr.AddClick(1, 100)

	// AddClick использует time.Now() напрямую, а nowFunc по умолчанию — тоже time.Now().
	// На границе суток UTC они теоретически могут попасть в разные дни,
	// поэтому принимаем сегодня или вчера.
	now := time.Now().UTC()
	today := tr.GetByDate(now)
	yesterday := tr.GetByDate(now.AddDate(0, 0, -1))

	if len(today)+len(yesterday) != 1 {
		t.Fatalf("expected click in today or yesterday, got today=%+v yesterday=%+v",
			today, yesterday)
	}
}

// --- Конкурентность (запускать с -race) ---

func TestTracker_ConcurrentAccess(t *testing.T) {
	// Критично: go test -race ./...
	now := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	tr := fixedTracker(now)

	const (
		writers = 10
		readers = 5
		users   = 100
		reads   = 200
	)

	var wg sync.WaitGroup

	for a := 0; a < writers; a++ {
		wg.Add(1)
		go func(author int64) {
			defer wg.Done()
			for u := 0; u < users; u++ {
				tr.AddClickAt(author, int64(u), now)
			}
		}(int64(a))
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < reads; i++ {
				_ = tr.GetByDate(now)
			}
		}()
	}

	wg.Wait()

	stats := tr.GetByDate(now)
	if len(stats) != writers {
		t.Fatalf("expected %d authors, got %d", writers, len(stats))
	}
	for _, s := range stats {
		if s.Count != users {
			t.Errorf("author %d: expected count=%d, got %d",
				s.AuthorID, users, s.Count)
		}
	}
}
