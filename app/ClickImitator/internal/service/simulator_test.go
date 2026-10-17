package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

// --- test doubles ---

// mockSender — потокобезопасный мок domain.ClickSender.
// Если индекс попытки < len(errs) — возвращает ошибку, иначе успех.
type mockSender struct {
	mu    sync.Mutex
	calls []domain.ClickEvent
	errs  []error
}

func (m *mockSender) Send(_ context.Context, ev domain.ClickEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, ev)
	i := len(m.calls) - 1
	if i < len(m.errs) {
		return m.errs[i]
	}
	return nil
}

func (m *mockSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockSender) snapshot() []domain.ClickEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.ClickEvent, len(m.calls))
	copy(out, m.calls)
	return out
}

// mockStrategy — мок Strategy для тестов Simulator.Run.
type mockStrategy struct {
	name          string
	users         int64
	runFn         func(ctx context.Context, sender domain.ClickSender, cfg config.Sender) error
	called        bool
	gotMaxRetries int
}

func (m *mockStrategy) Name() string      { return m.name }
func (m *mockStrategy) TotalUsers() int64 { return m.users }
func (m *mockStrategy) Run(ctx context.Context, sender domain.ClickSender, cfg config.Sender) error {
	m.called = true
	m.gotMaxRetries = cfg.MaxRetries
	if m.runFn != nil {
		return m.runFn(ctx, sender, cfg)
	}
	return nil
}

// --- helpers ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func withFastBackoff(t *testing.T) {
	t.Helper()
	old := initialSendBackoff
	initialSendBackoff = time.Millisecond
	t.Cleanup(func() { initialSendBackoff = old })
}

func retryCfg(n int) config.Sender {
	return config.Sender{MaxRetries: n, SendTimeout: time.Second}
}

// --- sendWithRetry ---

func TestSendWithRetry_SuccessFirstAttempt(t *testing.T) {
	sender := &mockSender{}
	req := domain.ClickEvent{UserID: 1, AuthorID: 2}

	err := sendWithRetry(context.Background(), sender, req, retryCfg(1), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

func TestSendWithRetry_RetriesThenSucceeds(t *testing.T) {
	withFastBackoff(t)
	sender := &mockSender{
		errs: []error{errors.New("fail1"), errors.New("fail2")},
	}
	req := domain.ClickEvent{UserID: 1}

	err := sendWithRetry(context.Background(), sender, req, retryCfg(3), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1-я попытка — fail1, 2-я — fail2, 3-я — успех.
	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3", sender.count())
	}
}

func TestSendWithRetry_ExhaustsRetries(t *testing.T) {
	withFastBackoff(t)
	sender := &mockSender{
		errs: []error{errors.New("e1"), errors.New("e2"), errors.New("e3")},
	}
	req := domain.ClickEvent{UserID: 1}

	err := sendWithRetry(context.Background(), sender, req, retryCfg(3), discardLogger())
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3", sender.count())
	}
}

// Отмена до старта: ни одной попытки, ошибки нет (graceful).
func TestSendWithRetry_ContextCancelledBeforeStart(t *testing.T) {
	sender := &mockSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sendWithRetry(ctx, sender, domain.ClickEvent{UserID: 1}, retryCfg(1), discardLogger())
	if err != nil {
		t.Fatalf("expected nil (graceful abort), got %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0", sender.count())
	}
}

// Отмена во время паузы между попытками — graceful abort, без исчерпания ретраев.
func TestSendWithRetry_ContextCancelledDuringRetries(t *testing.T) {
	old := initialSendBackoff
	initialSendBackoff = time.Second
	t.Cleanup(func() { initialSendBackoff = old })

	sender := &mockSender{errs: []error{
		errors.New("e1"), errors.New("e2"), errors.New("e3"),
		errors.New("e4"), errors.New("e5"),
	}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := sendWithRetry(ctx, sender, domain.ClickEvent{UserID: 1}, retryCfg(5), discardLogger())
	if err != nil {
		t.Fatalf("expected nil (graceful abort), got %v", err)
	}
	// Первая попытка отправлена, дальше отмена прерывает backoff-select.
	if got := sender.count(); got < 1 || got >= 5 {
		t.Fatalf("calls = %d, want in [1, 4]", got)
	}
}

// MaxRetries <= 0 нормализуется в 1 внутри sendWithRetry.
func TestSendWithRetry_MaxRetriesLowerBound(t *testing.T) {
	sender := &mockSender{errs: []error{errors.New("fail")}}
	cfg := config.Sender{MaxRetries: 0, SendTimeout: time.Second}

	err := sendWithRetry(context.Background(), sender, domain.ClickEvent{UserID: 1}, cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

// --- NewSimulator ---

func TestNewSimulator_NormalizesRetries(t *testing.T) {
	strat := &mockStrategy{}
	sim := NewSimulator(strat, &mockSender{}, config.Sender{MaxRetries: -5, SendTimeout: time.Second}, discardLogger())
	_ = sim.Run(context.Background())

	if strat.gotMaxRetries != 1 {
		t.Fatalf("maxRetries = %d, want 1", strat.gotMaxRetries)
	}
}

// --- Simulator.Run ---

func TestSimulator_Run_CallsStrategy(t *testing.T) {
	strat := &mockStrategy{name: "test", users: 3}
	sim := NewSimulator(strat, &mockSender{}, retryCfg(1), discardLogger())

	if err := sim.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strat.called {
		t.Fatal("strategy.Run was not called")
	}
	if strat.gotMaxRetries != 1 {
		t.Fatalf("maxRetries = %d, want 1", strat.gotMaxRetries)
	}
}

func TestSimulator_Run_StrategyErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	strat := &mockStrategy{
		name: "test",
		runFn: func(context.Context, domain.ClickSender, config.Sender) error {
			return errors.New("strategy failed")
		},
	}
	sim := NewSimulator(strat, &mockSender{}, retryCfg(1), logger)

	err := sim.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from Simulator.Run")
	}
	if !bytes.Contains(buf.Bytes(), []byte("simulation stopped with error")) {
		t.Fatalf("expected error log, got: %s", buf.String())
	}
}

func TestSimulator_Run_InterruptedLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	strat := &mockStrategy{
		name:  "test",
		runFn: func(context.Context, domain.ClickSender, config.Sender) error { return nil },
	}
	sim := NewSimulator(strat, &mockSender{}, retryCfg(1), logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sim.Run(ctx)
	if err != nil {
		t.Fatalf("expected nil on interrupted run, got %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("simulation interrupted")) {
		t.Fatalf("expected interrupted log, got: %s", buf.String())
	}
}

// Отмена контекста имеет приоритет над ошибкой стратегии:
// SIGTERM во время прогона не должен давать exit code 1.
func TestSimulator_Run_ContextCanceledTakesPriority(t *testing.T) {
	strat := &mockStrategy{
		name: "test",
		runFn: func(context.Context, domain.ClickSender, config.Sender) error {
			return errors.New("some send failure")
		},
	}
	sim := NewSimulator(strat, &mockSender{}, retryCfg(1), discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sim.Run(ctx); err != nil {
		t.Fatalf("expected nil (ctx canceled wins over strategy error), got %v", err)
	}
}
