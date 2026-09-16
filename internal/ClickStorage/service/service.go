package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Repository interface {
	GetAuthors(ctx context.Context) ([]int, error)
	UpsertStatsBatch(ctx context.Context, date time.Time, stats map[int]int) error
	GetStatsForDate(ctx context.Context, date time.Time) (map[int]int, error)
}

type Service struct {
	repo       Repository
	httpClient *http.Client
	statsURL   string
	batchSize  int
	maxRetries int
}

func New(repo Repository, statsURL string, batchSize, maxRetries int) *Service {
	if batchSize <= 0 {
		batchSize = 100
	}
	if maxRetries < 0 {
		maxRetries = 0
	}
	return &Service{
		repo:       repo,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		statsURL:   statsURL,
		batchSize:  batchSize,
		maxRetries: maxRetries,
	}
}

// UpdateStatsForDate получает данные из внешнего сервиса порциями
// и сохраняет в БД только те значения, которые больше уже имеющихся.
func (s *Service) UpdateStatsForDate(ctx context.Context, date time.Time) error {
	log.Printf("Service: starting update for date %s", date.Format("2006-01-02"))

	authors, err := s.repo.GetAuthors(ctx)
	if err != nil {
		return fmt.Errorf("get authors: %w", err)
	}
	if len(authors) == 0 {
		log.Printf("Service: no authors found, skipping")
		return nil
	}
	log.Printf("Service: fetched %d authors, batch size %d, max retries %d",
		len(authors), s.batchSize, s.maxRetries)

	for start := 0; start < len(authors); start += s.batchSize {
		end := start + s.batchSize
		if end > len(authors) {
			end = len(authors)
		}
		batch := authors[start:end]

		stats, err := s.fetchStats(ctx, batch)
		if err != nil {
			return fmt.Errorf("fetch stats (batch %d-%d): %w", start, end, err)
		}

		if err := s.repo.UpsertStatsBatch(ctx, date, stats); err != nil {
			return fmt.Errorf("upsert stats (batch %d-%d): %w", start, end, err)
		}

		log.Printf("Service: batch %d-%d saved (%d records)", start, end, len(stats))
	}

	log.Printf("Service: update for %s completed", date.Format("2006-01-02"))
	return nil
}

func (s *Service) fetchStats(ctx context.Context, authorIDs []int) (map[int]int, error) {
	ids := make([]string, len(authorIDs))
	for i, id := range authorIDs {
		ids[i] = strconv.Itoa(id)
	}

	q := url.Values{}
	q.Set("author_ids", strings.Join(ids, ","))
	reqURL := s.statsURL + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("external service returned %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Stats []struct {
			AuthorID int `json:"author_id"`
			Count    int `json:"count"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, string(body))
	}

	result := make(map[int]int, len(response.Stats))
	for _, item := range response.Stats {
		result[item.AuthorID] = item.Count
	}
	return result, nil
}

// doWithRetry выполняет запрос с экспоненциальным backoff + full jitter.
// Ретраит только сетевые ошибки, 5xx и 429.
func (s *Service) doWithRetry(req *http.Request) (*http.Response, error) {
	const (
		baseDelay = 200 * time.Millisecond
		maxDelay  = 3 * time.Second
	)

	var lastErr error

	for attempt := 0; attempt <= s.maxRetries; attempt++ {
		// req нельзя переиспользовать после Do — клонируем.
		r := req.Clone(req.Context())
		resp, err := s.httpClient.Do(r)

		// Успех или не-ретраибельный статус — отдаём как есть.
		if err == nil && !shouldRetryStatus(resp.StatusCode) {
			return resp, nil
		}

		if err != nil {
			lastErr = err
			// Контекст отменён — дальше смысла нет.
			if ctxErr := req.Context().Err(); ctxErr != nil {
				return nil, fmt.Errorf("request canceled: %w", ctxErr)
			}
		} else {
			// 5xx / 429 — дочитываем и закрываем тело, чтобы не текло.
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
		}

		if attempt == s.maxRetries {
			break
		}

		// full jitter: random(0, min(maxDelay, base * 2^attempt))
		capDelay := baseDelay << attempt
		if capDelay > maxDelay || capDelay <= 0 {
			capDelay = maxDelay
		}
		sleep := time.Duration(rand.Int63n(int64(capDelay)))

		log.Printf("Service: retry %d/%d after %v (last error: %v)",
			attempt+1, s.maxRetries, sleep, lastErr)

		select {
		case <-time.After(sleep):
		case <-req.Context().Done():
			return nil, fmt.Errorf("request canceled during backoff: %w", req.Context().Err())
		}
	}

	return nil, fmt.Errorf("after %d attempts: %w", s.maxRetries+1, lastErr)
}

func shouldRetryStatus(code int) bool {
	if code >= 500 && code <= 599 {
		return true
	}
	return code == http.StatusTooManyRequests // 429
}
