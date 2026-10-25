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
	"go.services.communication.dzen/app/ClickDashboard/pkg/config"
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
	client             StorageGatewayClient
	store              *Store
	logger             *slog.Logger
	maxDays            int
	refreshInterval    time.Duration
	startupRetryDelays []time.Duration
}

// NewStatsService принимает свою часть конфига — config.Projection.
func NewStatsService(
	client StorageGatewayClient,
	store *Store,
	logger *slog.Logger,
	cfg config.Projection,
) *StatsService {
	return &StatsService{
		client:             client,
		store:              store,
		logger:             logger,
		maxDays:            cfg.MaxDays,
		refreshInterval:    cfg.RefreshInterval,
		startupRetryDelays: cfg.StartupRetryDelays,
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
	refreshed := false
	attempts := 1 + len(s.startupRetryDelays)
	for attempt := 1; attempt <= attempts; attempt++ {
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
		if attempt == attempts {
			break
		}
		delay := s.startupRetryDelays[attempt-1]
		s.logger.Warn("initial refresh failed",
			"err", err, "attempt", attempt, "max", attempts, "retry_in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
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

	today := time.Now().UTC().Truncate(24 * time.Hour)
	from := today.AddDate(0, 0, -(s.maxDays - 1))
	to := today

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

// buildSnapshot собирает иммутабельный Snapshot из сырых данных:
// даты → индекс кликов → авторы (с синтезом заглушек) → строки таблицы.
func buildSnapshot(
	authors []domain.Author,
	statsByDate map[string][]domain.AuthorStats,
	from, to time.Time,
	maxDays int,
) *Snapshot {
	dates := dateRange(from, to, maxDays)
	clicks := indexClicks(statsByDate)
	ids, byID := resolveAuthors(authors, clicks)
	rows := buildRows(ids, byID, clicks, dates)

	return &Snapshot{
		Dates:   dates,
		Authors: rows,
		MaxDays: maxDays,
		BuiltAt: time.Now(),
	}
}

// dateRange возвращает даты по убыванию: to (свежайшая слева), to-1, ..., from.
func dateRange(from, to time.Time, maxDays int) []string {
	dates := make([]string, 0, maxDays)
	for d := to; !d.Before(from); d = d.AddDate(0, 0, -1) {
		dates = append(dates, d.Format("2006-01-02"))
	}
	return dates
}

// indexClicks строит индекс: authorID → dateStr → clicks.
func indexClicks(statsByDate map[string][]domain.AuthorStats) map[int64]map[string]int64 {
	clicks := make(map[int64]map[string]int64, len(statsByDate))
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
	return clicks
}

// resolveAuthors - Добавляет к существующим авторам временных
// если их нет в бд, но есть статистика по ним
func resolveAuthors(
	authors []domain.Author,
	clicks map[int64]map[string]int64,
) ([]int64, map[int64]domain.Author) {
	byID := make(map[int64]domain.Author, len(authors))
	ids := make([]int64, 0, len(authors))
	for _, a := range authors {
		if _, ok := byID[a.ID]; !ok {
			ids = append(ids, a.ID)
		}
		byID[a.ID] = a
	}
	for authorID := range clicks {
		if _, ok := byID[authorID]; !ok {
			ids = append(ids, authorID)
			byID[authorID] = domain.Author{
				ID:        authorID,
				FirstName: "unknown",
				LastName:  "unknown",
				Email:     fmt.Sprintf("unknown_%d@unknown.local", authorID),
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, byID
}

// buildRows собирает строки таблицы дашборда в порядке ids:
func buildRows(
	ids []int64,
	byID map[int64]domain.Author,
	clicks map[int64]map[string]int64,
	dates []string,
) []domain.AuthorRow {
	rows := make([]domain.AuthorRow, len(ids))
	for i, id := range ids {
		a := byID[id]
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
	return rows
}
