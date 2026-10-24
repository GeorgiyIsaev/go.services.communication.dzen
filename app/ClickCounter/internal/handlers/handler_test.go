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
	if !strings.Contains(body, "internal error") {
		t.Fatalf("body = %q, want to contain \"internal error\"", body)
	}
	for _, secret := range []string{"storagegateway", "8080", "10.0.3.5", "dial tcp", "connection refused"} {
		if strings.Contains(body, secret) {
			t.Fatalf("raw sender error leaked into response: %q (found %q)", body, secret)
		}
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "manual flush failed") {
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
	if want := utcYesterday(); !sender.gotDate.Equal(want) {
		t.Fatalf("date = %v, want %v", sender.gotDate, want)
	}
	if !bytes.Contains(logBuf.Bytes(), []byte("manual flush done")) {
		t.Fatalf("missing success log, got: %s", logBuf.String())
	}
}
