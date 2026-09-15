package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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

func (r *repo) SaveStats(ctx context.Context, authorID int, date time.Time, clicks int) error {
	log.Printf("DB: saving stats author=%d date=%s clicks=%d", authorID, date.Format("2006-01-02"), clicks)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO stats (author_id, date, clicks) VALUES ($1, $2, $3)
		 ON CONFLICT (author_id, date) DO UPDATE SET clicks = EXCLUDED.clicks`,
		authorID, date, clicks,
	)
	if err != nil {
		log.Printf("DB: error saving stats author=%d date=%s: %v", authorID, date.Format("2006-01-02"), err)
		return fmt.Errorf("save stats: %w", err)
	}
	log.Printf("DB: successfully saved stats author=%d date=%s", authorID, date.Format("2006-01-02"))
	return nil
}

func (r *repo) StatsExistForDate(ctx context.Context, date time.Time) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stats WHERE date = $1`, date,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check stats exist: %w", err)
	}
	exists := count > 0
	log.Printf("DB: stats for date %s exist? %v (count=%d)", date.Format("2006-01-02"), exists, count)
	return exists, nil
}

func (r *repo) GetStatsForDate(ctx context.Context, date time.Time) (map[int]int, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT author_id, clicks FROM stats WHERE date = $1`, date,
	)
	if err != nil {
		return nil, fmt.Errorf("get stats for date: %w", err)
	}
	defer rows.Close()

	result := make(map[int]int)
	for rows.Next() {
		var authorID, clicks int
		if err := rows.Scan(&authorID, &clicks); err != nil {
			return nil, fmt.Errorf("scan stats: %w", err)
		}
		result[authorID] = clicks
	}
	return result, rows.Err()
}

func (r *repo) SaveStatsBatch(ctx context.Context, date time.Time, stats map[int]int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO stats (author_id, date, clicks)
        VALUES ($1, $2, $3)
        ON CONFLICT (author_id, date)
        DO UPDATE SET clicks = EXCLUDED.clicks`)
	if err != nil {
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()

	for authorID, clicks := range stats {
		if _, err := stmt.ExecContext(ctx, authorID, date, clicks); err != nil {
			return fmt.Errorf("save stats author %d: %w", authorID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
