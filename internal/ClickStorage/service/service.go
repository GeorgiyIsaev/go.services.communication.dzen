// internal/StatsKeeper/service/service.go
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func (s *Service) UpdateStatsForDate(ctx context.Context, date time.Time) error {
	log.Printf("Service: starting update for date %s", date.Format("2006-01-02"))

	exists, err := s.repo.StatsExistForDate(ctx, date)
	if err != nil {
		log.Printf("Service: error checking stats exist: %v", err)
		return fmt.Errorf("check stats exist: %w", err)
	}
	if exists {
		log.Printf("Service: stats for %s already exist, skipping", date.Format("2006-01-02"))
		return nil
	}

	authors, err := s.repo.GetAuthors(ctx)
	if err != nil {
		log.Printf("Service: error getting authors: %v", err)
		return fmt.Errorf("get authors: %w", err)
	}
	if len(authors) == 0 {
		log.Printf("Service: no authors found, skipping")
		return nil
	}
	log.Printf("Service: fetched %d authors", len(authors))

	strIDs := make([]string, len(authors))
	for i, id := range authors {
		strIDs[i] = strconv.Itoa(id)
	}
	url := fmt.Sprintf("%s?author_ids=%s", s.statsURL, strings.Join(strIDs, ","))
	log.Printf("Service: requesting URL: %s", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Printf("Service: error creating request: %v", err)
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("Service: HTTP request failed: %v", err)
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Service: error reading response body: %v", err)
		return fmt.Errorf("read response body: %w", err)
	}
	bodyStr := string(bodyBytes)
	log.Printf("Service: response status: %d, body: %s", resp.StatusCode, bodyStr)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("external service returned %d: %s", resp.StatusCode, bodyStr)
	}

	// Парсим ожидаемый формат: {"stats":[{"author_id":1,"count":25}, ...]}
	var response struct {
		Stats []struct {
			AuthorID int `json:"author_id"`
			Count    int `json:"count"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		log.Printf("Service: failed to parse response: %v", err)
		return fmt.Errorf("decode response: %w (body: %s)", err, bodyStr)
	}

	raw := make(map[string]int)
	for _, item := range response.Stats {
		raw[strconv.Itoa(item.AuthorID)] = item.Count
	}
	log.Printf("Service: received stats for %d authors", len(raw))

	for key, clicks := range raw {
		authorID, err := strconv.Atoi(key)
		if err != nil {
			log.Printf("Service: invalid author ID key '%s', skipping", key)
			continue
		}
		log.Printf("Service: saving author %d clicks %d for date %s", authorID, clicks, date.Format("2006-01-02"))
		if err := s.repo.SaveStats(ctx, authorID, date, clicks); err != nil {
			log.Printf("Service: error saving stats for author %d: %v", authorID, err)
			return fmt.Errorf("save stats for author %d: %w", authorID, err)
		}
	}
	log.Printf("Service: successfully updated stats for %s", date.Format("2006-01-02"))
	return nil
}
