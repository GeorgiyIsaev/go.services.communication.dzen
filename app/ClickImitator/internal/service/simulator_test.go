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
	runFn         func(ctx context.Context, sender domain.ClickSender, maxRetries int) error
	called        bool
	gotMaxRetries int
}

func (m *mockStrategy) Name() string      { return m.name }
func (m *mockStrategy) TotalUsers() int64 { return m.users }
func (m *mockStrategy) Run(ctx context.Context, sender domain.ClickSender, maxRetries int) error {
	m.called = true
	m.gotMaxRetries = maxRetries
	if m.runFn != nil {
		return m.runFn(ctx, sender, maxRetries)
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

// --- sendWithRetry ---

func TestSendWithRetry_SuccessFirstAttempt(t *testing.T) {
	sender := &mockSender{}
	req := domain.ClickEvent{UserID: 1, AuthorID: 2}

	sendWithRetry(context.Background(), sender, req, 3, discardLogger())

	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

func TestSendWithRetry_RetriesThenSucceeds(t *testing.T) {
	withFastBackoff(t)
	sender := &mockSender{
		errs: []error{errors.New("fail1"), errors.New("fail2")},
	}

	sendWithRetry(context.Background(), sender, domain.ClickEvent{UserID: 1}, 5, discardLogger())

	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3", sender.count())
	}
}

func TestSendWithRetry_ExhaustsRetries(t *testing.T) {
	withFastBackoff(t)
	sender := &mockSender{
		errs: []error{errors.New("e1"), errors.New("e2"), errors.New("e3")},
	}

	sendWithRetry(context.Background(), sender, domain.ClickEvent{UserID: 1}, 3, discardLogger())

	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3", sender.count())
	}
}

func TestSendWithRetry_ContextCancelledBeforeStart(t *testing.T) {
	sender := &mockSender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sendWithRetry(ctx, sender, domain.ClickEvent{UserID: 1}, 3, discardLogger())

	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0", sender.count())
	}
}

// Отмена во время паузы между попытками не должна приводить к исчерпанию ретраев.
func TestSendWithRetry_ContextCancelledDuringRetries(t *testing.T) {
	old := initialSendBackoff
	initialSendBackoff = 100 * time.Millisecond
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

	sendWithRetry(ctx, sender, domain.ClickEvent{UserID: 1}, 5, discardLogger())

	// При отмене либо backoff-select, либо проверка ctx.Err() в начале итерации
	// прерывают цикл, поэтому попыток должно быть меньше maxRetries.
	// Точное число зависит от планировщика — проверяем именно свойство «не исчерпали».
	if got := sender.count(); got == 0 || got >= 5 {
		t.Fatalf("calls = %d, want in [1, 4]", got)
	}
}

// maxRetries <= 0 нормализуется в 1 внутри sendWithRetry.
func TestSendWithRetry_MaxRetriesLowerBound(t *testing.T) {
	sender := &mockSender{errs: []error{errors.New("fail")}}

	sendWithRetry(context.Background(), sender, domain.ClickEvent{UserID: 1}, 0, discardLogger())

	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

// --- NewSimulator ---

func TestNewSimulator_DefaultMaxRetries(t *testing.T) {
	strat := &mockStrategy{}
	sim := NewSimulator(strat, &mockSender{}, 0, discardLogger())
	sim.Run(context.Background())

	if strat.gotMaxRetries != 1 {
		t.Fatalf("maxRetries = %d, want 1", strat.gotMaxRetries)
	}
}

// --- Simulator.Run ---

func TestSimulator_Run_CallsStrategy(t *testing.T) {
	strat := &mockStrategy{name: "test", users: 3}
	sim := NewSimulator(strat, &mockSender{}, 2, discardLogger())

	sim.Run(context.Background())

	if !strat.called {
		t.Fatal("strategy.Run was not called")
	}
	if strat.gotMaxRetries != 2 {
		t.Fatalf("maxRetries = %d, want 2", strat.gotMaxRetries)
	}
}

func TestSimulator_Run_StrategyErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	strat := &mockStrategy{
		name: "test",
		runFn: func(context.Context, domain.ClickSender, int) error {
			return errors.New("strategy failed")
		},
	}
	sim := NewSimulator(strat, &mockSender{}, 1, logger)

	sim.Run(context.Background())

	if !bytes.Contains(buf.Bytes(), []byte("simulation stopped with error")) {
		t.Fatalf("expected error log, got: %s", buf.String())
	}
}

func TestSimulator_Run_InterruptedLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	strat := &mockStrategy{
		name: "test",
		// Возвращаем nil, но ctx уже отменён — Simulator должен залогировать "interrupted".
		runFn: func(context.Context, domain.ClickSender, int) error { return nil },
	}
	sim := NewSimulator(strat, &mockSender{}, 1, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sim.Run(ctx)

	if !bytes.Contains(buf.Bytes(), []byte("simulation interrupted")) {
		t.Fatalf("expected interrupted log, got: %s", buf.String())
	}
}
