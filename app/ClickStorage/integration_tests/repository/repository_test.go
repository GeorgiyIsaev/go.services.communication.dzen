//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestUpsertStatsBatch(t *testing.T) {
	db, _ := setupTestDB(t)

	r := New(db).(*repo) // приводим к конкретному типу, чтобы получить доступ к методам
	ctx := context.Background()
	testDate := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)

	// --- Шаг 1: вставка в пустую таблицу ---
	stats := map[int]int{1: 10, 2: 20, 3: 30}
	if err := r.UpsertStatsBatch(ctx, testDate, stats); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}
	assertStatsInDB(t, db, testDate, stats)

	// --- Шаг 2: попытка вставить МЕНЬШИЕ значения (должны быть проигнорированы) ---
	lowerStats := map[int]int{1: 5, 2: 15, 3: 25}
	if err := r.UpsertStatsBatch(ctx, testDate, lowerStats); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}
	// Ожидаем, что в БД остались СТАРЫЕ (большие) значения
	assertStatsInDB(t, db, testDate, stats)

	// --- Шаг 3: вставка БОЛЬШИХ значений (должны перезаписать) ---
	higherStats := map[int]int{1: 50, 2: 60, 3: 70}
	if err := r.UpsertStatsBatch(ctx, testDate, higherStats); err != nil {
		t.Fatalf("third upsert failed: %v", err)
	}
	assertStatsInDB(t, db, testDate, higherStats)
}

// assertStatsInDB читает stats для указанной даты и сверяет с ожидаемыми.
func assertStatsInDB(t *testing.T, db interface {
	QueryRow(query string, args ...any) *sql.Row
}, date time.Time, expected map[int]int) {
	t.Helper()

	for authorID, expectedClicks := range expected {
		var clicks int
		err := db.QueryRow(
			`SELECT clicks FROM stats WHERE author_id = $1 AND date = $2`,
			authorID, date,
		).Scan(&clicks)
		if err != nil {
			t.Fatalf("author %d: failed to query: %v", authorID, err)
		}
		if clicks != expectedClicks {
			t.Errorf("author %d: expected clicks %d, got %d", authorID, expectedClicks, clicks)
		}
	}
}
