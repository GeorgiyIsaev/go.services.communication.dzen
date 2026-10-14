package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
)

var (
	ErrInvalidStats = errors.New("invalid stats")
	ErrInvalidDate  = errors.New("invalid date: date is required")
	ErrDateInFuture = errors.New("invalid date: date cannot be in the future")
)

// StatsRepository - интерфейс репозитория
type StatsRepository interface {
	UpsertStats(ctx context.Context, date time.Time, stats []domain.AuthorStats) error
	GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
	GetAuthors(ctx context.Context) ([]domain.Author, error)
}

// StorageService — конкретная реализация бизнес-слоя.
type StorageService struct {
	repo StatsRepository
	now  func() time.Time
}

func NewStorageService(repo StatsRepository) *StorageService {
	return &StorageService{repo: repo, now: time.Now}
}

// SaveStats принимает статистику от ClickCounter и сохраняет её за указанную дату.
func (s *StorageService) SaveStats(ctx context.Context, stats []domain.AuthorStats, date time.Time) error {
	if date.IsZero() {
		return ErrInvalidDate
	}
	if err := validateStats(stats); err != nil {
		return err
	}

	targetDate := date.UTC().Truncate(24 * time.Hour)
	if targetDate.After(s.now().UTC().Truncate(24 * time.Hour)) {
		return ErrDateInFuture
	}

	if err := s.repo.UpsertStats(ctx, targetDate, stats); err != nil {
		return fmt.Errorf("save stats: %w", err)
	}
	return nil
}

func (s *StorageService) GetDashboardData(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	if date.IsZero() {
		return nil, ErrInvalidDate
	}
	result, err := s.repo.GetStatsByDate(ctx, date.UTC().Truncate(24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("get dashboard data: %w", err)
	}
	return result, nil
}

func (s *StorageService) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	authors, err := s.repo.GetAuthors(ctx)
	if err != nil {
		return nil, fmt.Errorf("get authors: %w", err)
	}
	return authors, nil
}

// validateStats проверяет входные данные от ClickCounter.
func validateStats(stats []domain.AuthorStats) error {
	if len(stats) == 0 {
		return fmt.Errorf("%w: empty stats list", ErrInvalidStats)
	}
	for i, s := range stats {
		if s.AuthorID <= 0 {
			return fmt.Errorf("%w: stats[%d].author_id must be positive, got %d", ErrInvalidStats, i, s.AuthorID)
		}
		if s.Count < 0 {
			return fmt.Errorf("%w: stats[%d].count must be non-negative, got %d", ErrInvalidStats, i, s.Count)
		}
	}
	return nil
}
