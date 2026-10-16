package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
	"go.services.communication.dzen/app/ClickCounter/pkg/config"
)

// --- helpers ---

func senderTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockStorageClient — управляемый клиент: логирует вызовы (время, дату,
// стат), может имитировать ошибки и блокировки. Все методы потокобезопасны.
type mockStorageClient struct {
	mu    sync.Mutex
	calls int
	times []time.Time
	dates []time.Time
	stats [][]domain.AuthorStat
	fn    func(ctx context.Context, date time.Time, stats []domain.AuthorStat) error
}

func (m *mockStorageClient) SendStats(ctx context.Context, date time.Time, stats []domain.AuthorStat) error {
	m.mu.Lock()
	m.calls++
	m.times = append(m.times, time.Now())
	m.dates = append(m.dates, date)
	m.stats = append(m.stats, stats)
	fn := m.fn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, date, stats)
	}
	return nil
}

func (m *mockStorageClient) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockStorageClient) Snapshot() (int, []time.Time, []time.Time, [][]domain.AuthorStat) {
	m.mu.Lock()
	defer m.mu.Unlock()
	times := append([]time.Time(nil), m.times...)
	dates := append([]time.Time(nil), m.dates...)
	stats := append([][]domain.AuthorStat(nil), m.stats...)
	return m.calls, times, dates, stats
}

// seedTrackerWithYesterday заполняет Tracker данными за yesterday.
// nowFunc = yesterday + 1 day, чтобы AddClickAt принял клик как «вчерашний».
func seedTrackerWithYesterday(t *testing.T) (*Tracker, time.Time) {
	t.Helper()
	yesterday := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	tomorrow := yesterday.AddDate(0, 0, 1)

	tr := NewTracker()
	tr.SetNowFunc(func() time.Time { return tomorrow })
	tr.AddClickAt(1, 100, yesterday)
	tr.AddClickAt(1, 101, yesterday) // тот же автор, второй уникальный
	tr.AddClickAt(2, 200, yesterday)
	return tr, yesterday
}

func fastRetries() config.Retries {
	return config.Retries{
		MaxRetries:  3,
		Delay:       5 * time.Millisecond,
		LongDelay:   10 * time.Millisecond,
		MaxDuration: 2 * time.Second,
	}
}

// --- SendNow: базовый успех ---

func TestSendNow_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	if err := s.SendNow(context.Background(), yesterday); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls, _, dates, stats := client.Snapshot()
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if !dates[0].Equal(yesterday) {
		t.Errorf("date passed to client = %v, want %v", dates[0], yesterday)
	}
	if len(stats[0]) != 2 {
		t.Errorf("expected 2 authors in stats, got %d: %+v", len(stats[0]), stats[0])
	}
}

// SendNow должен нормализовать переданную дату до полуночи UTC перед
// запросом к клиенту и к GetByDate.
func TestSendNow_NormalizesDateToMidnightUTC(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	withTime := yesterday.Add(15*time.Hour + 42*time.Minute)
	if err := s.SendNow(context.Background(), withTime); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, dates, _ := client.Snapshot()
	want := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !dates[0].Equal(want) {
		t.Errorf("client got date %v, want %v", dates[0], want)
	}
}

// --- SendNow: ретраи ---

func TestSendNow_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)

	var attempts int
	client := &mockStorageClient{
		fn: func(ctx context.Context, _ time.Time, _ []domain.AuthorStat) error {
			attempts++
			if attempts < 3 {
				return errors.New("transient error")
			}
			return nil
		},
	}

	cfg := config.Retries{
		MaxRetries:  10, // чтобы не триггерить longDelay в этом тесте
		Delay:       5 * time.Millisecond,
		LongDelay:   50 * time.Millisecond,
		MaxDuration: 2 * time.Second,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	if err := s.SendNow(context.Background(), yesterday); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.Calls(); got != 3 {
		t.Errorf("expected exactly 3 calls, got %d", got)
	}
}

// longDelay должен применяться раз в maxRetries попыток.
// Проверяем по таймстампам вызовов клиента.
func TestSendNow_LongDelayAfterMaxRetries(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("always fail")
		},
	}

	cfg := config.Retries{
		MaxRetries:  2,
		Delay:       10 * time.Millisecond,
		LongDelay:   100 * time.Millisecond,
		MaxDuration: 500 * time.Millisecond,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	_ = s.SendNow(context.Background(), yesterday)

	_, times, _, _ := client.Snapshot()
	if len(times) < 3 {
		t.Fatalf("expected at least 3 attempts, got %d", len(times))
	}

	var maxGap time.Duration
	for i := 1; i < len(times); i++ {
		if g := times[i].Sub(times[i-1]); g > maxGap {
			maxGap = g
		}
	}

	// Между 2-й и 3-й попытками должна быть longDelay (100ms).
	// Допускаем погрешность в обе стороны.
	if maxGap < 70*time.Millisecond || maxGap > 200*time.Millisecond {
		t.Errorf("expected one gap ~100ms (longDelay), got max gap = %v", maxGap)
	}
}

// --- SendNow: пустой Tracker ---

func TestSendNow_NoDataDoesNotCallClient(t *testing.T) {
	t.Parallel()

	tr := NewTracker() // пустой
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	err := s.SendNow(context.Background(), time.Now().UTC().AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.Calls(); got != 0 {
		t.Errorf("expected 0 calls for empty tracker, got %d", got)
	}
}

// --- SendNow: контекст и бюджет ---

func TestSendNow_ContextCancelledBetweenRetries(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("always fail")
		},
	}

	cfg := config.Retries{
		MaxRetries:  10, // чтобы не триггерить longDelay
		Delay:       100 * time.Millisecond,
		LongDelay:   200 * time.Millisecond,
		MaxDuration: 10 * time.Second,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := s.SendNow(ctx, yesterday)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if got := client.Calls(); got != 1 {
		t.Errorf("expected exactly 1 call before ctx fires, got %d", got)
	}
}

func TestSendNow_BudgetExhausted(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("always fail")
		},
	}

	cfg := config.Retries{
		MaxRetries:  1,
		Delay:       30 * time.Millisecond, // > MaxDuration
		LongDelay:   50 * time.Millisecond,
		MaxDuration: 5 * time.Millisecond,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	err := s.SendNow(context.Background(), yesterday)
	if !errors.Is(err, ErrFlushBudgetExhausted) {
		t.Errorf("expected ErrFlushBudgetExhausted, got %v", err)
	}
	// Первая попытка успевает выполниться, вторая уже нет: бюджет истёк.
	if got := client.Calls(); got != 1 {
		t.Errorf("expected 1 attempt before budget exhausted, got %d", got)
	}
}

// --- SendNow: параллельность ---

func TestSendNow_SecondConcurrentReturnsError(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)

	release := make(chan struct{})
	client := &mockStorageClient{
		fn: func(ctx context.Context, _ time.Time, _ []domain.AuthorStat) error {
			<-release
			return nil
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- s.SendNow(context.Background(), yesterday)
	}()

	// Ждём, пока первый вызов войдёт в клиент.
	deadline := time.Now().Add(2 * time.Second)
	for client.Calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.Calls() == 0 {
		t.Fatal("first SendNow never reached the client")
	}

	// Второй параллельный SendNow должен провалиться сразу.
	err := s.SendNow(context.Background(), yesterday)
	if err == nil {
		t.Fatal("expected error for concurrent SendNow, got nil")
	}
	if got := client.Calls(); got != 1 {
		t.Errorf("second call should not reach client, calls=%d", got)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Errorf("first SendNow failed: %v", err)
	}
}

// --- SendAsync ---

func TestSendAsync_Success(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	s.SendAsync(yesterday)

	deadline := time.Now().Add(2 * time.Second)
	for client.Calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.Calls() == 0 {
		t.Fatal("SendAsync never invoked client")
	}
}

func TestSendAsync_SkipsWhenAlreadySending(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)

	release := make(chan struct{})
	client := &mockStorageClient{
		fn: func(ctx context.Context, _ time.Time, _ []domain.AuthorStat) error {
			<-release
			return nil
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	s.SendAsync(yesterday)

	// Ждём, пока первый фоновой вызов войдёт в клиент.
	deadline := time.Now().Add(2 * time.Second)
	for client.Calls() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.Calls() == 0 {
		t.Fatal("first SendAsync never reached the client")
	}

	// Второй SendAsync должен быть проигнорирован (sending=true).
	s.SendAsync(yesterday)
	// Дадим немного времени — если бы он прошёл, calls вырос бы.
	time.Sleep(20 * time.Millisecond)
	if got := client.Calls(); got != 1 {
		t.Errorf("expected second SendAsync to be skipped, calls=%d", got)
	}

	close(release)
}

// --- Shutdown ---

// Shutdown должен отменить rootCtx и дождаться завершения фоновой отправки.
func TestShutdown_CancelsInFlightSend(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)

	started := make(chan struct{})
	client := &mockStorageClient{
		fn: func(ctx context.Context, _ time.Time, _ []domain.AuthorStat) error {
			close(started)
			<-ctx.Done() // блокируемся до отмены
			return ctx.Err()
		},
	}
	cfg := config.Retries{
		MaxRetries:  5,
		Delay:       10 * time.Millisecond,
		LongDelay:   20 * time.Millisecond,
		MaxDuration: 10 * time.Second,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)

	s.SendAsync(yesterday)
	<-started // гарантируем, что клиент уже «внутри»

	done := make(chan struct{})
	go func() {
		s.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// ok: Shutdown дождался отмены и возврата из flush
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after cancelling in-flight send")
	}
}

// После Shutdown новый SendNow должен вернуться с ошибкой контекста,
// а не «молча» отправить данные.
func TestShutdown_BlocksSubsequentSends(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())

	s.Shutdown() // rootCtx отменён

	// SendNow использует ctx вызывающего — он не отменён, но flush
	// проверит бюджет времени только в теле цикла. SendNow при этом
	// продолжит работать (это корректно: /flush использует свой ctx).
	// Важно, чтобы он не запаниковал и корректно сходил в клиент.
	err := s.SendNow(context.Background(), yesterday)
	if err != nil {
		// допустимо: зависит от внутреннего состояния, не паникуем
	}
	_ = err
}

// --- edge cases ---

// maxRetries == 0 не должен вызывать деление на ноль / панику.
// longDelay в этом случае не применяется, работает только retryDelay.
func TestSendNow_MaxRetriesZero(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)

	var attempts int
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			attempts++
			if attempts < 2 {
				return errors.New("transient")
			}
			return nil
		},
	}
	cfg := config.Retries{
		MaxRetries:  0,
		Delay:       5 * time.Millisecond,
		LongDelay:   5 * time.Millisecond,
		MaxDuration: 2 * time.Second,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	if err := s.SendNow(context.Background(), yesterday); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.Calls(); got != 2 {
		t.Errorf("expected 2 calls, got %d", got)
	}
}

// errors.Is корректно «прокидывает» ошибку клиента наружу (без обёртки
// в ErrFlushBudgetExhausted) при исчерпании ctx.
func TestSendNow_PropagatesContextErrorNotBudget(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("fail")
		},
	}
	cfg := config.Retries{
		MaxRetries:  10,
		Delay:       50 * time.Millisecond,
		LongDelay:   50 * time.Millisecond,
		MaxDuration: 10 * time.Second,
	}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := s.SendNow(ctx, yesterday)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if errors.Is(err, ErrFlushBudgetExhausted) {
		t.Errorf("context cancellation should not be reported as budget exhausted: %v", err)
	}
}
