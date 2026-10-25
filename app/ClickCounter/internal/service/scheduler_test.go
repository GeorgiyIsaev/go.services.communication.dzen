package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// --- untilNextFrom:
func TestUntilNextFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		now    time.Time
		hours  []int
		minute int
		want   time.Duration
	}{
		{
			name:   "before first hour → today",
			now:    time.Date(2024, 6, 15, 8, 0, 0, 0, time.UTC),
			hours:  []int{10, 15},
			minute: 30,
			want:   2*time.Hour + 30*time.Minute,
		},
		{
			name:   "between hours → next hour",
			now:    time.Date(2024, 6, 15, 11, 0, 0, 0, time.UTC),
			hours:  []int{10, 15},
			minute: 0,
			want:   4 * time.Hour,
		},
		{
			name:   "after last hour → tomorrow first hour",
			now:    time.Date(2024, 6, 15, 20, 0, 0, 0, time.UTC),
			hours:  []int{10, 15},
			minute: 0,
			want:   14 * time.Hour,
		},
		{
			name:   "exactly at hour:minute → tomorrow (rounds up)",
			now:    time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC),
			hours:  []int{10},
			minute: 30,
			want:   24 * time.Hour,
		},
		{
			name:   "one second past hour:minute → tomorrow",
			now:    time.Date(2024, 6, 15, 10, 30, 1, 0, time.UTC),
			hours:  []int{10},
			minute: 30,
			want:   24*time.Hour - time.Second,
		},
		{
			name:   "one second before hour:minute → 1 second",
			now:    time.Date(2024, 6, 15, 10, 29, 59, 0, time.UTC),
			hours:  []int{10},
			minute: 30,
			want:   time.Second,
		},
		{
			name:   "multiple hours picks nearest",
			now:    time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			hours:  []int{12, 9, 18},
			minute: 0,
			want:   2 * time.Hour,
		},
		{
			name:   "unsorted hours",
			now:    time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC),
			hours:  []int{15, 11, 20, 12},
			minute: 0,
			want:   time.Hour,
		},
		{
			name:   "duplicate hours",
			now:    time.Date(2024, 6, 15, 8, 0, 0, 0, time.UTC),
			hours:  []int{10, 10, 10},
			minute: 0,
			want:   2 * time.Hour,
		},
		{
			name:   "single hour",
			now:    time.Date(2024, 6, 15, 7, 0, 0, 0, time.UTC),
			hours:  []int{9},
			minute: 0,
			want:   2 * time.Hour,
		},
		{
			name:   "midnight configured, now is start of day → tomorrow",
			now:    time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
			hours:  []int{0},
			minute: 0,
			want:   24 * time.Hour,
		},
		{
			name:   "midnight configured, now is 23:59 → 1 minute",
			now:    time.Date(2024, 6, 15, 23, 59, 0, 0, time.UTC),
			hours:  []int{0},
			minute: 0,
			want:   time.Minute,
		},
		{
			name:   "sub-second precision",
			now:    time.Date(2024, 6, 15, 10, 29, 59, 500_000_000, time.UTC),
			hours:  []int{10},
			minute: 30,
			want:   500 * time.Millisecond,
		},
		{
			name:   "empty hours documents current behavior (BUG: returns 0)",
			now:    time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
			hours:  nil,
			minute: 0,
			want:   0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := untilNextFrom(tt.now, tt.hours, tt.minute)
			if got != tt.want {
				t.Errorf("untilNextFrom(%s, %v, %d) = %v, want %v",
					tt.now.Format(time.RFC3339Nano), tt.hours, tt.minute, got, tt.want)
			}
		})
	}
}

// Инвариант: для непустого списка часов результат всегда 0 < d <= 24h.
func TestUntilNextFrom_PositiveAndBounded(t *testing.T) {
	t.Parallel()

	hours := []int{0, 6, 12, 18}
	minute := 30
	start := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 24*60; i += 7 {
		now := start.Add(time.Duration(i) * time.Minute)
		d := untilNextFrom(now, hours, minute)
		if d <= 0 || d > 24*time.Hour {
			t.Errorf("untilNextFrom(%s) = %v, want 0 < d <= 24h",
				now.Format(time.RFC3339), d)
		}
	}
}

// Тонкая обёртка над untilNextFrom + time.Now() — проверяем только контракт.
func TestUntilNext(t *testing.T) {
	hours := []int{0, 12}
	minute := 0

	d := untilNext(hours, minute)
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("untilNext = %v, want 0 < d <= 24h", d)
	}
}

// --- helpers для RunScheduler ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// noopClient — клиент, который ничего не делает. Используется, чтобы собрать
// настоящий *Sender без реального HTTP.
type noopClient struct{}

func (noopClient) SendStats(context.Context, time.Time, []domain.AuthorStat) error {
	return nil
}

func newTestSenderForScheduler() *Sender {
	tracker := NewTracker()
	cfg := config.Retries{
		Delays: []time.Duration{5 * time.Millisecond},
	}
	return NewSender(tracker, noopClient{}, testLogger(), cfg)
}

// --- RunScheduler ---

func TestRunScheduler_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	sender := newTestSenderForScheduler()
	defer sender.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Scheduler{Hours: []int{12}, Minute: 0}

	done := make(chan struct{})
	go func() {
		RunScheduler(ctx, sender, testLogger(), cfg)
		close(done)
	}()

	// Даём scheduler'у войти в цикл ожидания на time.After(wait).
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok: RunScheduler вернулся после отмены ctx.
	case <-time.After(2 * time.Second):
		t.Fatal("RunScheduler did not return after ctx cancel")
	}
}

func TestRunScheduler_AlreadyCancelledContext(t *testing.T) {
	t.Parallel()

	sender := newTestSenderForScheduler()
	defer sender.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // сразу отменяем

	cfg := config.Scheduler{Hours: []int{3, 15}, Minute: 30}

	done := make(chan struct{})
	go func() {
		RunScheduler(ctx, sender, testLogger(), cfg)
		close(done)
	}()

	select {
	case <-done:
		// ok: scheduler должен увидеть ctx.Done() в первом же select.
	case <-time.After(2 * time.Second):
		t.Fatal("RunScheduler did not return on already-cancelled ctx")
	}
}

// Сценарий: если wait == 0 (например, конфиг пустой — известный баг),
// scheduler уходит в tight-loop по time.After(0), но всё равно должен
// корректно завершиться по отмене ctx. Тест документирует поведение.
func TestRunScheduler_EmptyHoursStillStopsOnCancel(t *testing.T) {
	t.Parallel()

	sender := newTestSenderForScheduler()
	defer sender.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.Scheduler{Hours: nil, Minute: 0}

	done := make(chan struct{})
	go func() {
		RunScheduler(ctx, sender, testLogger(), cfg)
		close(done)
	}()

	// Короткая пауза — scheduler успеет «прокрутиться» несколько раз.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunScheduler did not stop on cancel with empty hours")
	}
}
