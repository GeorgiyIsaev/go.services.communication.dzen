package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/internal/service"
)

// mockSender — ручной мок Sender.
type mockSender struct {
	err     error
	called  bool
	gotDate time.Time
}

func (m *mockSender) SendNow(_ context.Context, date time.Time) error {
	m.called = true
	m.gotDate = date
	return m.err
}

// mockTracker пустой TrackerReader
type mockTracker struct{}

func (mockTracker) GetByDate(time.Time) []domain.AuthorStat { return nil }

func newTestStatsHandler(sender Sender, logBuf *bytes.Buffer) *StatsHandler {
	logger := slog.New(slog.NewTextHandler(logBuf, nil))
	return NewStatsHandler(sender, mockTracker{}, logger)
}

func doFlush(t *testing.T, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/flush/yesterday", nil))
	return rr
}

// не светим детали sender'а наружу, только 500 + лог
func TestFlush_ServiceErrorDoesNotLeak(t *testing.T) {
	rawErr := errors.New(`Post "http://storagegateway:8080/stats": dial tcp 10.0.3.5:8080: connect: connection refused`)

	var logBuf bytes.Buffer
	sender := &mockSender{err: rawErr}
	h := newTestStatsHandler(sender, &logBuf)

	rr := doFlush(t, h.FlushYesterday)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%q)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "flush failed, retrying in background") {
		t.Fatalf("body = %q, want retry-in-background message", body)
	}
	for _, secret := range []string{"storagegateway", "8080", "10.0.3.5", "dial tcp", "connection refused"} {
		if strings.Contains(body, secret) {
			t.Fatalf("raw sender error leaked into response: %q (found %q)", body, secret)
		}
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "manual flush failed, retrying in background") {
		t.Fatalf("missing failure log, got: %s", logs)
	}
	if !strings.Contains(logs, "dial tcp") {
		t.Fatalf("raw error not logged, got: %s", logs)
	}
}

// Успешный путь не задет правкой: 200 и корректная дата.
func TestFlush_Success(t *testing.T) {
	var logBuf bytes.Buffer
	sender := &mockSender{}
	h := newTestStatsHandler(sender, &logBuf)

	want := utcYesterday() // до запроса: хендлер вычислит «вчера» чуть позже
	rr := doFlush(t, h.FlushYesterday)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"status":"sent"`) {
		t.Fatalf("body = %q, want status sent", rr.Body.String())
	}
	if !sender.called {
		t.Fatal("sender was not called")
	}
	// utcYesterday() вызывается дважды (тест и хендлер), поэтому сравниваем
	// с допуском: дата должна совпадать с «вчера», а не до наносекунды.
	if d := sender.gotDate.Sub(want); d < -5*time.Second || d > 5*time.Second {
		t.Fatalf("date = %v, want ~%v (diff %v)", sender.gotDate, want, d)
	}
	if sender.gotDate.UTC().Format("2006-01-02") != want.UTC().Format("2006-01-02") {
		t.Fatalf("date day = %v, want %v", sender.gotDate, want)
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("manual flush done")) {
		t.Fatalf("missing success log, got: %s", logBuf.String())
	}
}

// Параллельный flush (идёт фоновой/ручной) — 503, без запуска второго.
func TestFlush_AlreadyInProgressReturns503(t *testing.T) {
	var logBuf bytes.Buffer
	sender := &mockSender{err: service.ErrFlushInProgress}
	h := newTestStatsHandler(sender, &logBuf)

	rr := doFlush(t, h.FlushYesterday)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%q)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "flush already in progress") {
		t.Fatalf("body = %q", rr.Body.String())
	}
}
