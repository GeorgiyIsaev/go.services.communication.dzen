package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.services.communication.dzen/internal/ClickGenerator/domain"
)

// HTTPClickSender реализует интерфейс domain.ClickSender.
type HTTPClickSender struct {
	client *http.Client
	url    string
}

func NewHTTPClickSender(url string) *HTTPClickSender {
	return &HTTPClickSender{
		// Таймаут критически важен, чтобы горутины не зависали при проблемах с сетью
		client: &http.Client{Timeout: 5 * time.Second},
		url:    url,
	}
}

func (h *HTTPClickSender) Send(ctx context.Context, req domain.ClickRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ошибка маршалинга: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ошибка HTTP-запроса: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("неожиданный статус ответа: %d", resp.StatusCode)
	}

	return nil
}
