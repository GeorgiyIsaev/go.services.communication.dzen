// internal/StatsKeeper/service/service.go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.services.communication.dzen/internal/ClickStorage/repository"
)

type Service struct {
	repo       repository.Repository
	httpClient *http.Client
	statsURL   string
}

func New(repo repository.Repository, statsURL string) *Service {
	return &Service{
		repo:       repo,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		statsURL:   statsURL,
	}
}

// UpdateStatsForDate обновляет статистику для указанной даты.
// Обычно date = yesterday (текущая дата - 1 день).
// Если данные уже есть, пропускаем запрос (проверка внутри).
func (s *Service) UpdateStatsForDate(ctx context.Context, date time.Time) error {
	// 1. Проверяем, есть ли уже статистика за эту дату
	exists, err := s.repo.StatsExistForDate(ctx, date)
	if err != nil {
		return fmt.Errorf("check stats exist: %w", err)
	}
	if exists {
		// Уже есть, ничего не делаем
		return nil
	}

	// 2. Получаем список авторов
	authors, err := s.repo.GetAuthors(ctx)
	if err != nil {
		return fmt.Errorf("get authors: %w", err)
	}
	if len(authors) == 0 {
		// Нет авторов, выходим
		return nil
	}

	// 3. Формируем URL с параметром author_ids
	strIDs := make([]string, len(authors))
	for i, id := range authors {
		strIDs[i] = strconv.Itoa(id)
	}
	url := fmt.Sprintf("%s?author_ids=%s", s.statsURL, strings.Join(strIDs, ","))

	// 4. Выполняем GET-запрос
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
		return fmt.Errorf("external service returned %d: %s", resp.StatusCode, string(body))
	}

	var raw map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// 5. Сохраняем статистику для каждого автора
	for key, clicks := range raw {
		authorID, err := strconv.Atoi(key)
		if err != nil {
			continue // игнорируем некорректный ключ
		}
		if err := s.repo.SaveStats(ctx, authorID, date, clicks); err != nil {
			// Логируем ошибку, но продолжаем для других авторов
			// Можно вернуть ошибку, но лучше накапливать
			return fmt.Errorf("save stats for author %d: %w", authorID, err)
		}
	}
	return nil
}
