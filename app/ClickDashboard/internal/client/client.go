// Package client — HTTP-обёртка над StorageGateway.
//
// Особенности:
//   - ВСЕ методы принимают context.Context и прокидывают его в http.NewRequestWithContext.
//   - Пробрасывает заголовок X-Request-ID: если в ctx есть request_id,
//     он уйдёт в исходящий запрос, и в логах StorageGateway будет тот же ID.
//   - Не тянет тела 5xx-ответов наверх (там может быть что-то внутреннее).
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
	"go.services.communication.dzen/app/ClickDashboard/pkg/reqid"
)

var (
	// ErrUpstreamUnavailable — StorageGateway недоступен: сеть, DNS, timeout.
	ErrUpstreamUnavailable = errors.New("storage gateway unavailable")
	// ErrUpstreamBadResponse — StorageGateway ответил неожидаемым кодом или битым JSON.
	ErrUpstreamBadResponse = errors.New("storage gateway bad response")
)

// StorageGatewayClient — конкретная реализация HTTP-клиента к StorageGateway.
type StorageGatewayClient struct {
	baseURL string
	http    *http.Client
}

func NewStorageGatewayClient(baseURL string, timeout time.Duration) *StorageGatewayClient {
	return &StorageGatewayClient{
		baseURL: baseURL,
		http: &http.Client{
			// Timeout на ОДИН запрос. Общий таймаут /stats снаружи задаётся в хендлере.
			Timeout: timeout,
		},
	}
}

// GetAuthors дёргает GET /authors у StorageGateway.
func (c *StorageGatewayClient) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	var resp domain.AuthorsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/authors", &resp); err != nil {
		return nil, err
	}
	return resp.Authors, nil
}

// GetStatsByDate дёргает GET /dashboard?date=YYYY-MM-DD у StorageGateway.
func (c *StorageGatewayClient) GetStatsByDate(ctx context.Context, date time.Time) ([]domain.AuthorStats, error) {
	q := url.Values{}
	q.Set("date", date.Format("2006-01-02"))

	var resp domain.DashboardResponse
	if err := c.doJSON(ctx, http.MethodGet, "/dashboard?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return resp.Stats, nil
}

// doJSON — общий скелет запроса: собирает URL, ставит заголовки,
// прокидывает request_id, разбирает ответ, классифицирует ошибки.
func (c *StorageGatewayClient) doJSON(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// Пробрасываем request_id. Если его нет — не ставим заголовок,
	// StorageGateway сгенерирует свой и это будет другой трейс.
	if id := reqid.FromContext(ctx); id != "" {
		req.Header.Set(reqid.HeaderName, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Ошибки сети/DNS/timeout не показываем наверх как есть —
		// они могут содержать внутренние адреса и порты.
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrUpstreamBadResponse, resp.StatusCode)

	case resp.StatusCode >= 400:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: status %d, body: %s", ErrUpstreamBadResponse, resp.StatusCode, body)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUpstreamBadResponse, err)
	}
	return nil
}
