package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// UpsertStats атомарно сохраняет статистику за день.
// При конфликте (author_id, date) обновляет clicks, только если новое значение больше существующего
// Отсутствующие авторы создаются в той же транзакции.
func (r *PostgresRepository) UpsertStats(ctx context.Context, date time.Time, stats []domain.AuthorStats) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := ensureAuthors(ctx, tx, stats); err != nil {
		return fmt.Errorf("ensure authors: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO stats (author_id, date, clicks)
		VALUES ($1, $2, $3)
		ON CONFLICT (author_id, date)
		DO UPDATE SET clicks = EXCLUDED.clicks
		WHERE stats.clicks < EXCLUDED.clicks`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, s := range stats {
		if _, err := stmt.ExecContext(ctx, s.AuthorID, date, s.Count); err != nil {
			return fmt.Errorf("upsert author %d: %w", s.AuthorID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// GetStatsByDate возвращает статистику за день, отсортированную по author_id.
func (r *PostgresRepository) GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT author_id, clicks
		FROM stats
		WHERE date = $1
		ORDER BY author_id`, date)
	if err != nil {
		return nil, fmt.Errorf("query stats: %w", err)
	}
	defer rows.Close()

	result := make([]domain.AuthorStats, 0)
	for rows.Next() {
		var s domain.AuthorStats
		if err := rows.Scan(&s.AuthorID, &s.Count); err != nil {
			return nil, fmt.Errorf("scan stats row: %w", err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stats rows: %w", err)
	}
	return result, nil
}

// GetAuthors возвращает всех авторов (для ручки GET /authors).
func (r *PostgresRepository) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, first_name, last_name, email
		FROM authors
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query authors: %w", err)
	}
	defer rows.Close()

	result := make([]domain.Author, 0)
	for rows.Next() {
		var a domain.Author
		if err := rows.Scan(&a.ID, &a.FirstName, &a.LastName, &a.Email); err != nil {
			return nil, fmt.Errorf("scan author row: %w", err)
		}
		result = append(result, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate author rows: %w", err)
	}
	return result, nil
}

// ensureAuthors гарантирует наличие всех author_id из stats в таблице authors.
// Отсутствующие создаются с заглушками: first_name='unknown', last_name='unknown',
// email=unknown_<id>@unknown.local
func ensureAuthors(ctx context.Context, tx *sql.Tx, stats []domain.AuthorStats) error {
	for _, s := range stats {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO authors (id, email, first_name, last_name)
			VALUES ($1, $2, 'unknown', 'unknown')
			ON CONFLICT (id) DO NOTHING`,
			s.AuthorID,
			fmt.Sprintf("unknown_%d@unknown.local", s.AuthorID),
		)
		if err != nil {
			return fmt.Errorf("insert author %d: %w", s.AuthorID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		SELECT setval(
			pg_get_serial_sequence('authors', 'id'),
			GREATEST((SELECT COALESCE(MAX(id), 0) FROM authors), 1)
		)`); err != nil {
		return fmt.Errorf("setval authors_id_seq: %w", err)
	}

	return nil
}

// GetStatsRange возвращает статистику за диапазон дат включительно.
// Возвращает map[dateStr][]domain.AuthorStats, где dateStr = "YYYY-MM-DD".
func (r *PostgresRepository) GetStatsRange(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT date, author_id, clicks
		FROM stats
		WHERE date >= $1 AND date <= $2
		ORDER BY date, author_id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query stats range: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]domain.AuthorStats)
	for rows.Next() {
		var d time.Time
		var s domain.AuthorStats
		if err := rows.Scan(&d, &s.AuthorID, &s.Count); err != nil {
			return nil, fmt.Errorf("scan stats range row: %w", err)
		}
		dateStr := d.Format("2006-01-02")
		result[dateStr] = append(result[dateStr], s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stats range rows: %w", err)
	}
	return result, nil
}
