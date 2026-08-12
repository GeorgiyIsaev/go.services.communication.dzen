package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.services.communication.dzen/internal/ClickStorage_InMemory/storage"
)

// StatsService – интерфейс для работы со статистикой.
type StatsService interface {
	UpdateStats(ctx context.Context) error
	GetStats() map[int]int
	AddAuthor(ctx context.Context, id int) error
	RemoveAuthor(ctx context.Context, id int) error
}

// statsService реализует StatsService.
type statsService struct {
	repo       storage.Repository
	httpClient *http.Client
	statsURL   string
	mu         sync.RWMutex
	stats      map[int]int
}

func NewStatsService(repo storage.Repository, statsURL string) StatsService {
	return &statsService{
		repo:       repo,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		statsURL:   statsURL,
		stats:      make(map[int]int),
	}
}

func (s *statsService) UpdateStats(ctx context.Context) error {
	ids, err := s.repo.GetAuthorIDs(ctx)
	if err != nil {
		return fmt.Errorf("get author ids: %w", err)
	}
	if len(ids) == 0 {
		return nil // нечего обновлять
	}

	// Формируем URL с параметром author_ids
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.Itoa(id)
	}
	url := fmt.Sprintf("%s?author_ids=%s", s.statsURL, strings.Join(strIDs, ","))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("external returned %d: %s", resp.StatusCode, body)
	}

	var raw map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	newStats := make(map[int]int, len(raw))
	for k, v := range raw {
		id, err := strconv.Atoi(k)
		if err != nil {
			continue // игнорируем некорректные ключи
		}
		newStats[id] = v
	}

	s.mu.Lock()
	s.stats = newStats
	s.mu.Unlock()

	_ = s.repo.SaveStats(ctx, newStats) // заглушка
	return nil
}

func (s *statsService) GetStats() map[int]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make(map[int]int, len(s.stats))
	for k, v := range s.stats {
		res[k] = v
	}
	return res
}

func (s *statsService) AddAuthor(ctx context.Context, id int) error {
	return s.repo.AddAuthor(ctx, id)
}

func (s *statsService) RemoveAuthor(ctx context.Context, id int) error {
	return s.repo.RemoveAuthor(ctx, id)
}
