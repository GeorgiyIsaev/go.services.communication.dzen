//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// setupTestDB поднимает контейнер Postgres, накатывает миграции и возвращает *sql.DB.
// Функция регистрирует очистку через t.Cleanup, поэтому контейнер будет остановлен
// автоматически после завершения теста.
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	ctx := context.Background()

	// 1. Запускаем контейнер PostgreSQL
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("test-db"),
		postgres.WithUsername("test-user"),
		postgres.WithPassword("test-pass"),
		postgres.BasicWaitStrategies(), // ждём готовности БД
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	// 2. Формируем DSN для подключения
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// 3. Подключаемся к БД
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping db: %v", err)
	}

	// 4. Накатываем миграции из вашей директории.
	//    runtime.Caller(0) даёт путь к текущему файлу, от него строим относительный путь к миграциям.
	if err := applyMigrations(db); err != nil {
		db.Close()
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to apply migrations: %v", err)
	}

	// 5. Регистрируем очистку: закрыть БД и остановить контейнер.
	cleanup := func() {
		if err := db.Close(); err != nil {
			log.Printf("error closing db: %v", err)
		}
		if err := pgContainer.Terminate(ctx); err != nil {
			log.Printf("error terminating container: %v", err)
		}
	}
	t.Cleanup(cleanup)

	return db, cleanup
}

// applyMigrations накатывает все UP-миграции из internal/ClickStorage/migrations.
func applyMigrations(db *sql.DB) error {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("failed to get caller info")
	}

	// Тест лежит в internal/ClickStorage/repository,
	// миграции — в internal/ClickStorage/migrations (на уровень выше).
	baseDir := filepath.Dir(filename)
	migrationsPath := filepath.Join(baseDir, "..", "migrations")

	abs, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("abs migrations path: %w", err)
	}

	url := "file:" + filepath.ToSlash(abs)

	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("create migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(url, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrate instance (url=%s): %w", url, err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}

	return nil
}
