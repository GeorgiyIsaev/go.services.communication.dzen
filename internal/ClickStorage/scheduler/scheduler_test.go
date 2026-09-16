package scheduler

import (
	"testing"
	"time"
)

func TestUntilNextRunFrom(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "ровно полночь — до 00:10 осталось 10 минут",
			now:  time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
			want: 10 * time.Minute,
		},
		{
			name: "00:05 — до 00:10 осталось 5 минут",
			now:  time.Date(2026, 9, 18, 0, 5, 0, 0, time.UTC),
			want: 5 * time.Minute,
		},
		{
			name: "ровно 00:10 — граница, следующая цель 01:10",
			now:  time.Date(2026, 9, 18, 0, 10, 0, 0, time.UTC),
			want: 1 * time.Hour,
		},
		{
			name: "00:30 — до 01:10 осталось 40 минут",
			now:  time.Date(2026, 9, 18, 0, 30, 0, 0, time.UTC),
			want: 40 * time.Minute,
		},
		{
			name: "01:05 — до 01:10 осталось 5 минут",
			now:  time.Date(2026, 9, 18, 1, 5, 0, 0, time.UTC),
			want: 5 * time.Minute,
		},
		{
			name: "ровно 01:10 — граница, следующий запуск завтра в 00:10",
			now:  time.Date(2026, 9, 18, 1, 10, 0, 0, time.UTC),
			want: 22*time.Hour + 59*time.Minute + 60*time.Second,
		},
		{
			name: "середина дня — следующий запуск завтра в 00:10",
			now:  time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
			want: 12*time.Hour + 10*time.Minute,
		},
		{
			name: "23:59 — до завтрашних 00:10 осталось 11 минут",
			now:  time.Date(2026, 9, 18, 23, 59, 0, 0, time.UTC),
			want: 11 * time.Minute,
		},
		{
			name: "переход через конец месяца",
			now:  time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC),
			want: 11 * time.Minute,
		},
		{
			name: "переход через конец года",
			now:  time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC),
			want: 11 * time.Minute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := untilNextRunFrom(tc.now)
			if got != tc.want {
				t.Errorf("now=%s: want %v, got %v",
					tc.now.Format(time.RFC3339), tc.want, got)
			}
		})
	}
}
