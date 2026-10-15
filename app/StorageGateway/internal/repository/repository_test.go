package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
)

func newMockRepo(t *testing.T) (*PostgresRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	return NewPostgresRepository(db), mock, func() { _ = db.Close() }
}

// --- UpsertStats ---

func TestUpsertStats_Success(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	date := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	stats := []domain.AuthorStats{{AuthorID: 1, Count: 10}}

	mock.ExpectBegin()

	// ensureAuthors: insert автора
	mock.ExpectExec("INSERT INTO authors").
		WithArgs(int64(1), "unknown_1@unknown.local").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// ensureAuthors: подтянуть sequence
	mock.ExpectExec("SELECT setval").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// upsert статистики через prepared statement
	prep := mock.ExpectPrepare("INSERT INTO stats")
	prep.ExpectExec().
		WithArgs(int64(1), date, int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	if err := repo.UpsertStats(context.Background(), date, stats); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpsertStats_BeginTxError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectBegin().WillReturnError(errors.New("begin failed"))

	err := repo.UpsertStats(context.Background(), time.Now(), []domain.AuthorStats{{AuthorID: 1, Count: 1}})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpsertStats_EnsureAuthorsFails_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO authors").
		WillReturnError(errors.New("insert failed"))
	// при ошибке должен произойти откат
	mock.ExpectRollback()

	err := repo.UpsertStats(context.Background(), time.Now(), []domain.AuthorStats{{AuthorID: 1, Count: 1}})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpsertStats_UpsertExecFails_RollsBack(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	date := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	stats := []domain.AuthorStats{{AuthorID: 1, Count: 10}}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO authors").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT setval").WillReturnResult(sqlmock.NewResult(0, 1))

	prep := mock.ExpectPrepare("INSERT INTO stats")
	prep.ExpectExec().
		WithArgs(int64(1), date, int64(10)).
		WillReturnError(errors.New("exec failed"))

	mock.ExpectRollback()

	if err := repo.UpsertStats(context.Background(), date, stats); err == nil {
		t.Fatal("want error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// --- GetStatsByDate ---

func TestGetStatsByDate_Success(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	date := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{"author_id", "clicks"}).
		AddRow(int64(1), int64(10)).
		AddRow(int64(2), int64(20))

	mock.ExpectQuery("SELECT author_id, clicks").
		WithArgs(date).
		WillReturnRows(rows)

	got, err := repo.GetStatsByDate(context.Background(), date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].AuthorID != 1 || got[1].Count != 20 {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetStatsByDate_QueryError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectQuery("SELECT author_id, clicks").
		WillReturnError(errors.New("query failed"))

	if _, err := repo.GetStatsByDate(context.Background(), time.Now()); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestGetStatsByDate_ScanError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	// author_id ожидается как int64, кладём string — Scan должен упасть
	rows := sqlmock.NewRows([]string{"author_id", "clicks"}).
		AddRow("not-an-int", int64(10))

	mock.ExpectQuery("SELECT author_id, clicks").WillReturnRows(rows)

	if _, err := repo.GetStatsByDate(context.Background(), time.Now()); err == nil {
		t.Fatal("want scan error, got nil")
	}
}

// --- GetAuthors ---

func TestGetAuthors_Success(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"id", "first_name", "last_name", "email"}).
		AddRow(int64(1), "Ivan", "Petrov", "i@p.local").
		AddRow(int64(2), "Anna", "Sidorova", "a@s.local")

	mock.ExpectQuery("SELECT id, first_name, last_name, email").
		WillReturnRows(rows)

	got, err := repo.GetAuthors(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].FirstName != "Ivan" || got[1].Email != "a@s.local" {
		t.Fatalf("got %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetAuthors_QueryError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	mock.ExpectQuery("SELECT id, first_name, last_name, email").
		WillReturnError(errors.New("query failed"))

	if _, err := repo.GetAuthors(context.Background()); err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestGetAuthors_ScanError(t *testing.T) {
	repo, mock, cleanup := newMockRepo(t)
	defer cleanup()

	// id ожидается int64 — передаём nil (NULL), Scan упадёт
	rows := sqlmock.NewRows([]string{"id", "first_name", "last_name", "email"}).
		AddRow(nil, "Ivan", "Petrov", "i@p.local")

	mock.ExpectQuery("SELECT id, first_name, last_name, email").WillReturnRows(rows)

	if _, err := repo.GetAuthors(context.Background()); err == nil {
		t.Fatal("want scan error, got nil")
	}
}

// Заглушка, чтобы избежать неиспользуемого импорта database/sql,
// если в будущем понадобится сравнение типов.
var _ = sql.ErrNoRows
