package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/service"
)

type repo struct {
	db *sql.DB
}

func New(db *sql.DB) service.Repository {
	return &repo{db: db}
}

func (r *repo) GetAuthors(ctx context.Context) ([]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM authors ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("get authors: %w", err)
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan author: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// UpsertStatsBatch вставляет/обновляет статистику одной пачкой.
// Обновляет только если новое значение БОЛЬШЕ существующего —
// это делает сам Postgres атомарно, гонок нет.
func (r *repo) UpsertStatsBatch(ctx context.Context, date time.Time, stats map[int]int) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO stats (author_id, date, clicks)
		VALUES ($1, $2, $3)
		ON CONFLICT (author_id, date)
		DO UPDATE SET clicks = EXCLUDED.clicks
		WHERE stats.clicks < EXCLUDED.clicks`)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for authorID, clicks := range stats {
		if _, err := stmt.ExecContext(ctx, authorID, date, clicks); err != nil {
			return fmt.Errorf("upsert stats author %d: %w", authorID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
