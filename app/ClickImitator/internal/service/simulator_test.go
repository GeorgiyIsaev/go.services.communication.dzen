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
	calls []domain.ClickMessage
	errs  []error
}

func (m *mockSender) Send(_ context.Context, ev domain.ClickMessage) error {
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

func (m *mockSender) snapshot() []domain.ClickMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.ClickMessage, len(m.calls))
	copy(out, m.calls)
	return out
}

// mockStrategy — мок Strategy для тестов Simulator.Run.
type mockStrategy struct {
	name         string
	users        int64
	runFn        func(ctx context.Context, sender domain.ClickSender, cfg config.Sender) error
	called       bool
	gotSenderCfg config.Sender
}

func (m *mockStrategy) Name() string      { return m.name }
func (m *mockStrategy) TotalUsers() int64 { return m.users }
func (m *mockStrategy) Run(ctx context.Context, sender domain.ClickSender, cfg config.Sender) error {
	m.called = true
	m.gotSenderCfg = cfg
	if m.runFn != nil {
		return m.runFn(ctx, sender, cfg)
	}
	return nil
}

// --- helpers ---

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// retryCfg — конфиг на n попыток: 1 базовая + (n-1) пауз по 1ms.
func retryCfg(n int) config.Sender {
	delays := make([]time.Duration, 0, n-1)
	for i := 1; i < n; i++ {
		delays = append(delays, time.Millisecond)
	}
	return config.Sender{RetryDelays: delays, SendTimeout: time.Second}
}

// --- sendWithRetry ---

func TestSendWithRetry_SuccessFirstAttempt(t *testing.T) {
	sender := &mockSender{}
	req := domain.ClickMessage{UserID: 1, AuthorID: 2}

	err := sendWithRetry(context.Background(), sender, req, retryCfg(1), discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

func TestSendWithRetry_RetriesThenSucceeds(t *testing.T) {
	sender := &mockSender{
		errs: []error{errors.New("fail1"), errors.New("fail2")},
	}
	req := domain.ClickMessage{UserID: 1}

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
	sender := &mockSender{
		errs: []error{errors.New("e1"), errors.New("e2"), errors.New("e3")},
	}
	req := domain.ClickMessage{UserID: 1}

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

	err := sendWithRetry(ctx, sender, domain.ClickMessage{UserID: 1}, retryCfg(1), discardLogger())
	if err != nil {
		t.Fatalf("expected nil (graceful abort), got %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0", sender.count())
	}
}

// Отмена во время паузы между попытками — graceful abort, без исчерпания ретраев.
func TestSendWithRetry_ContextCancelledDuringRetries(t *testing.T) {
	// Большие паузы: отмена (20ms) гарантированно случится в середине ожидания.
	cfg := config.Sender{
		RetryDelays: []time.Duration{time.Second, time.Second, time.Second, time.Second},
		SendTimeout: time.Second,
	}
	sender := &mockSender{errs: []error{
		errors.New("e1"), errors.New("e2"), errors.New("e3"),
		errors.New("e4"), errors.New("e5"),
	}}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := sendWithRetry(ctx, sender, domain.ClickMessage{UserID: 1}, cfg, discardLogger())
	if err != nil {
		t.Fatalf("expected nil (graceful abort), got %v", err)
	}
	// Первая попытка отправлена, дальше отмена прерывает паузу расписания.
	if got := sender.count(); got < 1 || got >= 5 {
		t.Fatalf("calls = %d, want in [1, 4]", got)
	}
}

// Пустое расписание = ретраев нет: ровно одна попытка и ошибка.
func TestSendWithRetry_NoRetryDelaysSingleAttempt(t *testing.T) {
	sender := &mockSender{errs: []error{errors.New("fail")}}
	cfg := config.Sender{SendTimeout: time.Second}

	err := sendWithRetry(context.Background(), sender, domain.ClickMessage{UserID: 1}, cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}

// Паузы ретраев следуют конфигурируемому расписанию (40ms → 80ms), растут.
func TestSendWithRetry_UsesConfiguredGrowingDelays(t *testing.T) {
	sender := &mockSender{errs: []error{errors.New("e1"), errors.New("e2"), errors.New("e3")}}
	cfg := config.Sender{
		RetryDelays: []time.Duration{40 * time.Millisecond, 80 * time.Millisecond},
		SendTimeout: time.Second,
	}

	start := time.Now()
	err := sendWithRetry(context.Background(), sender, domain.ClickMessage{UserID: 1}, cfg, discardLogger())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after exhausted schedule, got nil")
	}
	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3 (1 + 2 по расписанию)", sender.count())
	}
	if elapsed < 120*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 120ms (сумма пауз 40+80)", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("elapsed = %v, looks hung", elapsed)
	}
}

// --- NewSimulator ---

// SendTimeout <= 0 нормализуется в 5s внутри NewSimulator; расписание
// ретраев пробрасывается стратегии без изменений.
func TestNewSimulator_NormalizesSendTimeout(t *testing.T) {
	strat := &mockStrategy{}
	wantDelays := []time.Duration{time.Millisecond, 2 * time.Millisecond}
	sim := NewSimulator(strat, &mockSender{},
		config.Sender{RetryDelays: wantDelays, SendTimeout: 0}, discardLogger())
	_ = sim.Run(context.Background())

	if strat.gotSenderCfg.SendTimeout != 5*time.Second {
		t.Fatalf("sendTimeout = %v, want 5s", strat.gotSenderCfg.SendTimeout)
	}
	if len(strat.gotSenderCfg.RetryDelays) != len(wantDelays) {
		t.Fatalf("retryDelays = %v, want %v", strat.gotSenderCfg.RetryDelays, wantDelays)
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
	if strat.gotSenderCfg.SendTimeout != time.Second {
		t.Fatalf("sender cfg not passed through: %+v", strat.gotSenderCfg)
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
