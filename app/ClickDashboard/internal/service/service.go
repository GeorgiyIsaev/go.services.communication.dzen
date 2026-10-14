package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
)

// Ошибки сервиса. Хендлер ловит именно их, не зная о внутренностях клиента.
var (
	ErrInvalidDays = errors.New("invalid days")
	ErrUpstream    = errors.New("storage gateway request failed")
)

// StorageGatewayClient — интерфейс
type StorageGatewayClient interface {
	GetAuthors(ctx context.Context) ([]domain.Author, error)
	GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error)
}

// StatsService агрегирует данные из StorageGateway в матрицу "авторы × дни".
type StatsService struct {
	client  StorageGatewayClient
	maxDays int
	// sem — семафор: ограничивает количество одновременных запросов к StorageGateway.
	// Канал с буфером N: чтобы "войти", нужно положить токен; выходя — забрать.
	sem chan struct{}
}

func NewStatsService(client StorageGatewayClient, maxDays, concurrency int) *StatsService {
	if concurrency <= 0 {
		concurrency = 5
	}
	return &StatsService{
		client:  client,
		maxDays: maxDays,
		sem:     make(chan struct{}, concurrency),
	}
}

// MaxDays возвращает текущий лимит дней (из конфига).
func (s *StatsService) MaxDays() int {
	return s.maxDays
}

// GetStats собирает матрицу кликов.
//
// Алгоритм:
//  1. Проверяем, что days в допустимом диапазоне [1, maxDays].
//  2. Формируем список дат: вчера, позавчера, ... (по убыванию).
//  3. Параллельно запрашиваем:
//     - список всех авторов (один запрос),
//     - статистику по каждой дате (N запросов).
//     Все запросы идут через семафор s.sem, не более N одновременно.
//  4. Собираем матрицу: автор → {дата → клики}. Пустые дни остаются нулями.
//  5. Формируем ответ в формате, удобном для фронта.
func (s *StatsService) GetStats(ctx context.Context, days int) (*domain.StatsMatrix, error) {
	if days < 1 {
		return nil, fmt.Errorf("%w: must be >= 1, got %d", ErrInvalidDays, days)
	}
	if days > s.maxDays {
		return nil, fmt.Errorf("%w: must be <= %d, got %d", ErrInvalidDays, s.maxDays, days)
	}

	// Список дат: вчера и назад. Сегодня не берём — данные за сегодня ещё собираются.
	yesterday := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	dates := make([]time.Time, 0, days)
	for i := 0; i < days; i++ {
		dates = append(dates, yesterday.AddDate(0, 0, -i))
	}

	// errgroup: g.Wait() вернёт первую ошибку, остальные запросы отменятся через gCtx.
	g, gCtx := errgroup.WithContext(ctx)

	// Параллельный запрос авторов.
	var authors []domain.Author
	g.Go(func() error {
		a, err := s.client.GetAuthors(gCtx)
		if err != nil {
			return fmt.Errorf("get authors: %w", err)
		}
		authors = a
		return nil
	})

	// Параллельные запросы статистики по дням.
	// statsByDate[i] соответствует dates[i]. Каждая горутина пишет в свой индекс — race-free.
	statsByDate := make([][]domain.AuthorStats, len(dates))
	for i, d := range dates {
		i, d := i, d // локальные копии для замыкания (Go < 1.22)
		g.Go(func() error {
			// Заходим в семафор или отменяемся, если контекст уже отменён.
			select {
			case s.sem <- struct{}{}:
				defer func() { <-s.sem }()
			case <-gCtx.Done():
				return gCtx.Err()
			}

			st, err := s.client.GetStatsByDate(gCtx, d)
			if err != nil {
				return fmt.Errorf("get stats %s: %w", d.Format("2006-01-02"), err)
			}
			statsByDate[i] = st
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		// Приводим ошибки клиента к ошибкам сервиса — наружу отдаём только ErrUpstream.
		return nil, wrapUpstream(err)
	}

	// Собираем матрицу кликов: map[authorID]map[dateStr]clicks.
	clicks := make(map[int64]map[string]int64, len(authors))
	for i, st := range statsByDate {
		dateStr := dates[i].Format("2006-01-02")
		for _, s := range st {
			if _, ok := clicks[s.AuthorID]; !ok {
				clicks[s.AuthorID] = make(map[string]int64)
			}
			clicks[s.AuthorID][dateStr] = s.Count
		}
	}

	// Авторы в ответе — по возрастанию ID (стабильно и предсказуемо).
	sort.Slice(authors, func(i, j int) bool { return authors[i].ID < authors[j].ID })

	dateStrs := make([]string, len(dates))
	for i, d := range dates {
		dateStrs[i] = d.Format("2006-01-02")
	}

	rows := make([]domain.AuthorRow, 0, len(authors))
	for _, a := range authors {
		row := domain.AuthorRow{
			ID:        a.ID,
			FirstName: a.FirstName,
			LastName:  a.LastName,
			Email:     a.Email,
			Clicks:    make([]int64, len(dates)),
		}
		if byDate, ok := clicks[a.ID]; ok {
			for i, ds := range dateStrs {
				row.Clicks[i] = byDate[ds] // нет записи → 0
			}
		}
		rows = append(rows, row)
	}

	return &domain.StatsMatrix{
		Dates:   dateStrs,
		Authors: rows,
		MaxDays: s.maxDays,
	}, nil
}

// wrapUpstream сворачивает ошибки клиента в ErrUpstream.
// Сервис не раскрывает детали наружу — их видно только в логах.
func wrapUpstream(err error) error {
	if err == nil {
		return nil
	}
	// Если это уже наша ошибка (ErrInvalidDays и т.п.) — не трогаем.
	if errors.Is(err, ErrInvalidDays) {
		return err
	}
	// Отмена контекста и таймауты — пробрасываем как есть,
	// хендлер умеет их распознавать отдельно.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUpstream, err)
}
