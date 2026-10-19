package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
)

// Ошибки сервиса.
var (
	ErrInvalidDays      = errors.New("invalid days")
	ErrSnapshotNotReady = errors.New("snapshot not ready")
)

// StorageGatewayClient — интерфейс (реализуется client.StorageGatewayClient).
type StorageGatewayClient interface {
	GetAuthors(ctx context.Context) ([]domain.Author, error)
	GetStatsRange(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error)
}

// StatsService держит проекцию в памяти и периодически её обновляет.
type StatsService struct {
	client          StorageGatewayClient
	store           *Store
	logger          *slog.Logger
	maxDays         int
	refreshInterval time.Duration
}

func NewStatsService(
	client StorageGatewayClient,
	store *Store,
	logger *slog.Logger,
	maxDays int,
	refreshInterval time.Duration,
) *StatsService {
	return &StatsService{
		client:          client,
		store:           store,
		logger:          logger,
		maxDays:         maxDays,
		refreshInterval: refreshInterval,
	}
}

func (s *StatsService) MaxDays() int { return s.maxDays }

// GetStats читает из памяти.
func (s *StatsService) GetStats(ctx context.Context, days int) (*domain.StatsMatrix, error) {
	if days < 1 {
		return nil, fmt.Errorf("%w: must be >= 1, got %d", ErrInvalidDays, days)
	}
	if days > s.maxDays {
		return nil, fmt.Errorf("%w: must be <= %d, got %d", ErrInvalidDays, s.maxDays, days)
	}

	snap := s.store.Get()
	if snap == nil {
		return nil, ErrSnapshotNotReady
	}
	return snap.Slice(days), nil
}

// Run — блокирующий цикл фоновых обновлений.
func (s *StatsService) Run(ctx context.Context) {
	const (
		startupRetries    = 6
		startupRetryDelay = 10 * time.Second
	)

	refreshed := false
	for i := 0; i < startupRetries; i++ {
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.Refresh(rctx)
		cancel()
		if err == nil {
			refreshed = true
			break
		}
		if ctx.Err() != nil {
			return
		}
		s.logger.Warn("initial refresh failed",
			"err", err, "attempt", i+1, "retry_in", startupRetryDelay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(startupRetryDelay):
		}
	}
	if !refreshed {
		s.logger.Error("initial refresh failed after retries; will keep retrying on ticker")
	}

	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rctx, cancel := context.WithTimeout(ctx, s.refreshInterval/2)
			err := s.Refresh(rctx)
			cancel()
			if err != nil {
				// Старый снапшот продолжаем отдавать: устаревшие данные
				// лучше, чем 5xx на каждом /stats.
				s.logger.Error("refresh failed", "err", err)
			}
		}
	}
}

// Refresh тянет данные из StorageGateway и публикует новый снапшот.
func (s *StatsService) Refresh(ctx context.Context) error {
	start := time.Now()

	// Диапазон: вчера и назад на maxDays-1. Сегодня не берём —
	// данные за сегодня ещё собираются.
	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	from := yesterday.AddDate(0, 0, -(s.maxDays - 1))
	to := yesterday

	var (
		authors     []domain.Author
		statsByDate map[string][]domain.AuthorStats
	)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		a, err := s.client.GetAuthors(gCtx)
		if err != nil {
			return fmt.Errorf("get authors: %w", err)
		}
		authors = a
		return nil
	})
	g.Go(func() error {
		st, err := s.client.GetStatsRange(gCtx, from, to)
		if err != nil {
			return fmt.Errorf("get stats range: %w", err)
		}
		statsByDate = st
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}

	snap := buildSnapshot(authors, statsByDate, from, to, s.maxDays)
	s.store.Publish(snap)

	s.logger.Info("snapshot refreshed",
		"authors", len(snap.Authors),
		"days", len(snap.Dates),
		"max_days", snap.MaxDays,
		"duration", time.Since(start),
	)
	return nil
}

// buildSnapshot собирает иммутабельный Snapshot из сырых данных.
func buildSnapshot(
	authors []domain.Author,
	statsByDate map[string][]domain.AuthorStats,
	from, to time.Time,
	maxDays int,
) *Snapshot {
	// Даты по убыванию: to (вчера), to-1, ..., from.
	dates := make([]string, 0, maxDays)
	for d := to; !d.Before(from); d = d.AddDate(0, 0, -1) {
		dates = append(dates, d.Format("2006-01-02"))
	}

	// Индекс: authorID → dateStr → clicks.
	clicks := make(map[int64]map[string]int64, len(authors))
	for dateStr, dayStats := range statsByDate {
		for _, st := range dayStats {
			m, ok := clicks[st.AuthorID]
			if !ok {
				m = make(map[string]int64)
				clicks[st.AuthorID] = m
			}
			m[dateStr] = st.Count
		}
	}

	sort.Slice(authors, func(i, j int) bool { return authors[i].ID < authors[j].ID })

	rows := make([]domain.AuthorRow, len(authors))
	for i, a := range authors {
		row := domain.AuthorRow{
			ID:        a.ID,
			FirstName: a.FirstName,
			LastName:  a.LastName,
			Email:     a.Email,
			Clicks:    make([]int64, len(dates)),
		}
		if byDate, ok := clicks[a.ID]; ok {
			for j, ds := range dates {
				row.Clicks[j] = byDate[ds] // нет записи → 0
			}
		}
		rows[i] = row
	}

	return &Snapshot{
		Dates:   dates,
		Authors: rows,
		MaxDays: maxDays,
		BuiltAt: time.Now(),
	}
}
