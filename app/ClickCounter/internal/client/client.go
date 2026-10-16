// Package client — HTTP-обёртка над StorageGateway.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/pkg/reqid"
)

var (
	ErrUpstreamUnavailable = errors.New("storage gateway unavailable")
	ErrUpstreamBadResponse = errors.New("storage gateway bad response")
)

// SendStatsRequest — тело POST /stats StorageGateway.
type SendStatsRequest struct {
	Date  string              `json:"date"` // "2006-01-02"
	Stats []domain.AuthorStat `json:"stats"`
}

type StorageGatewayClient struct {
	baseURL string
	http    *http.Client
}

func NewStorageGatewayClient(baseURL string, timeout time.Duration) *StorageGatewayClient {
	return &StorageGatewayClient{
		baseURL: baseURL,
		http: &http.Client{
			// Timeout на ОДИН запрос. Общее время ретраев контролируется в sender.
			Timeout: timeout,
		},
	}
}

// SendStats отправляет статистику за указанный день в StorageGateway.
// Успех = HTTP 2xx. Всё остальное — ошибка.
func (c *StorageGatewayClient) SendStats(ctx context.Context, date time.Time, stats []domain.AuthorStat) error {
	body, err := json.Marshal(SendStatsRequest{
		Date:  date.Format("2006-01-02"),
		Stats: stats,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/stats", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Проброс request_id для сквозной трассировки в логах StorageGateway.
	if id := reqid.FromContext(ctx); id != "" {
		req.Header.Set(reqid.HeaderName, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 500:
		// Не тянем тело 5xx — там может быть внутренняя информация.
		return fmt.Errorf("%w: status %d", ErrUpstreamBadResponse, resp.StatusCode)

	case resp.StatusCode >= 400:
		// 4xx безопасно показать (валидация).
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status %d, body: %s", ErrUpstreamBadResponse, resp.StatusCode, b)
	}

	return nil
}
