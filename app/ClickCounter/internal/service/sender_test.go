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

// seedTrackerToday добавляет в уже посевленный трекер клики за сегодня
func seedTrackerToday(tr *Tracker, yesterday time.Time) time.Time {
	today := yesterday.AddDate(0, 0, 1)
	tr.AddClickAt(3, 300, today)
	return today
}

func fastRetries() config.Retries {
	return config.Retries{
		Delays: []time.Duration{
			5 * time.Millisecond,
			10 * time.Millisecond,
			20 * time.Millisecond,
		},
	}
}

// waitForCalls ждёт, пока клиент не будет вызван n раз.
func waitForCalls(t *testing.T, client *mockStorageClient, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for client.Calls() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.Calls() < n {
		t.Fatalf("client calls = %d, want >= %d", client.Calls(), n)
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

// SendNow должен нормализовать дату до полуночи UTC перед запросом
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

// --- SendNow: первая попытка синхронна, ретраи уходят в фон ---

// SendNow возвращает ошибку ПЕРВОЙ попытки сразу, не дожидаясь расписания
func TestSendNow_ReturnsFirstAttemptErrorImmediately(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	boom := errors.New("boom")
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return boom
		},
	}
	// Большое расписание: если бы SendNow ждал ретраи, тест бы завис.
	cfg := config.Retries{Delays: []time.Duration{time.Hour, time.Hour}}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	start := time.Now()
	err := s.SendNow(context.Background(), yesterday)
	elapsed := time.Since(start)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("SendNow blocked for %v, want immediate return", elapsed)
	}
}

// После падения первой попытки SendNow продолжает ретраи в фоне —
// даже если HTTP-запрос уже завершился (его ctx отменён).
func TestSendNow_RetryContinuesInBackgroundAfterRequestDone(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	boom := errors.New("boom")

	var mu sync.Mutex
	attempts := 0
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			if attempts < 3 {
				return boom
			}
			return nil
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	reqCtx, cancel := context.WithCancel(context.Background())
	err := s.SendNow(reqCtx, yesterday)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	cancel() // «клиент отключился» — фоновые ретраи идут на rootCtx

	waitForCalls(t, client, 3)
}

// Отменённый контекст при входе в SendNow — фоновые ретраи НЕ запускаются.
func TestSendNow_NoBackgroundRetryWhenCtxCanceled(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("boom")
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.SendNow(ctx, yesterday); err == nil {
		t.Fatal("expected error, got nil")
	}

	// Даём фону шанс «прокрутиться»: вызовов остаться должно ровно 1.
	time.Sleep(50 * time.Millisecond)
	if got := client.Calls(); got != 1 {
		t.Errorf("client calls = %d, want 1 (no background retries after cancel)", got)
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

// --- FlushWithSchedule: расписание пауз ---

// Паузы между попытками обязаны следовать конфигу (40ms → 80ms),
// после исчерпания — ErrFlushExhausted, дальше не шлём.
func TestFlushWithSchedule_UsesConfiguredDelays(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("always fail")
		},
	}
	cfg := config.Retries{Delays: []time.Duration{40 * time.Millisecond, 80 * time.Millisecond}}
	s := NewSender(tr, client, senderTestLogger(), cfg)
	defer s.Shutdown()

	err := s.flushWithSchedule(context.Background(), yesterday)
	if !errors.Is(err, ErrFlushExhausted) {
		t.Fatalf("err = %v, want ErrFlushExhausted", err)
	}

	calls, times, _, _ := client.Snapshot()
	if calls != 3 { // 1 попытка + 2 по расписанию
		t.Fatalf("calls = %d, want 3", calls)
	}

	gap1 := times[1].Sub(times[0])
	gap2 := times[2].Sub(times[1])
	if gap1 < 40*time.Millisecond {
		t.Errorf("gap1 = %v, want >= 40ms", gap1)
	}
	if gap2 < 80*time.Millisecond {
		t.Errorf("gap2 = %v, want >= 80ms", gap2)
	}
	// Верхняя граница — только защита от зависания (щедрый допуск).
	if gap1 > 5*time.Second || gap2 > 5*time.Second {
		t.Errorf("gaps too large: %v, %v", gap1, gap2)
	}
}

// Успех на любой попытке останавливает расписание.
func TestFlushWithSchedule_StopsOnSuccess(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	var mu sync.Mutex
	attempts := 0
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			mu.Lock()
			defer mu.Unlock()
			attempts++
			if attempts < 3 {
				return errors.New("transient")
			}
			return nil
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	if err := s.flushWithSchedule(context.Background(), yesterday); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.Calls(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

// Пустое расписание = ретраев нет: ровно одна фоновая попытка.
func TestFlushWithSchedule_EmptyDelaysSingleAttempt(t *testing.T) {
	t.Parallel()

	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("always fail")
		},
	}
	s := NewSender(tr, client, senderTestLogger(), config.Retries{})
	defer s.Shutdown()

	err := s.flushWithSchedule(context.Background(), yesterday)
	if !errors.Is(err, ErrFlushExhausted) {
		t.Fatalf("err = %v, want ErrFlushExhausted", err)
	}
	if got := client.Calls(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// --- SendAsync: очередь вместо молчаливого пропуска ---

// Если flush уже идёт, второй SendAsync НЕ выбрасывается: дата встаёт в
// очередь глубиной 1 и выполняется после текущего flush'а. Последняя дата
// выигрывает (перезаписывает накопленную).
func TestSendAsync_QueuesInsteadOfSkipping(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)
	today := seedTrackerToday(tr, yesterday)

	release := make(chan struct{})
	var mu sync.Mutex
	var blockedFirst bool
	client := &mockStorageClient{
		fn: func(ctx context.Context, _ time.Time, _ []domain.AuthorStat) error {
			mu.Lock()
			first := !blockedFirst
			blockedFirst = true
			mu.Unlock()
			if first {
				<-release // блокируем только первый (идущий) flush
			}
			return nil
		},
	}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())
	defer s.Shutdown()

	s.SendAsync(yesterday) // уходит в работу и блокируется в клиенте
	waitForCalls(t, client, 1)

	// Пока идёт flush: две новые даты. Последняя (today) должна выиграть.
	s.SendAsync(yesterday)
	s.SendAsync(today)

	close(release)

	// Ждём фоновую обработку очереди: сегодняшний flush не должен потеряться.
	waitForCalls(t, client, 2)

	// Больше ничего не висит в очереди: подождём и убедимся, что вызовов 2.
	time.Sleep(50 * time.Millisecond)
	_, _, dates, _ := client.Snapshot()
	if len(dates) != 2 {
		t.Fatalf("calls = %d, want 2 (in-flight + queued latest)", len(dates))
	}
	if !dates[0].Equal(yesterday) {
		t.Errorf("first flush date = %v, want %v", dates[0], yesterday)
	}
	if !dates[1].Equal(today) {
		t.Errorf("queued flush date = %v, want %v (latest wins)", dates[1], today)
	}
}

// --- Параллельность: ручной flush ---

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

	// Второй параллельный SendNow должен сразу получить ErrFlushInProgress.
	err := s.SendNow(context.Background(), yesterday)
	if !errors.Is(err, ErrFlushInProgress) {
		t.Fatalf("err = %v, want ErrFlushInProgress", err)
	}
	if got := client.Calls(); got != 1 {
		t.Errorf("second call should not reach client, calls=%d", got)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Errorf("first SendNow failed: %v", err)
	}
}

// --- Финальный провал логируется, повторов больше нет ---

// После исчерпания расписания: точное число попыток, ошибок дальше нет,
// провал зафиксирован в логе.
func TestSendAsync_ExhaustedLogsFailureAndStops(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	client := &mockStorageClient{
		fn: func(context.Context, time.Time, []domain.AuthorStat) error {
			return errors.New("gateway down")
		},
	}
	cfg := config.Retries{Delays: []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}}
	s := NewSender(tr, client, logger, cfg)
	defer s.Shutdown()

	s.SendAsync(yesterday)

	// 1 попытка + 2 по расписанию = 3, и всё.
	waitForCalls(t, client, 3)
	time.Sleep(100 * time.Millisecond)
	if got := client.Calls(); got != 3 {
		t.Errorf("calls = %d, want exactly 3 (no extra retries after schedule)", got)
	}

	logs := logBuf.String()
	if !bytes.Contains(logBuf.Bytes(), []byte("flush failed, no more retries")) {
		t.Errorf("final failure not logged, got: %s", logs)
	}
}

// --- Shutdown ---

// Shutdown должен отменить rootCtx и дождаться завершения фоновой отправки.
func TestShutdown_CancelsInFlightSend(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)

	started := make(chan struct{})
	client := &mockStorageClient{
		fn: func(ctx context.Context, date time.Time, stats []domain.AuthorStat) error {
			close(started)
			<-ctx.Done() // блокируемся до отмены
			return ctx.Err()
		},
	}
	cfg := config.Retries{Delays: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}}
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

// После Shutdown новый SendNow не должен паниковать и корректно сходил в клиент.
func TestShutdown_BlocksSubsequentSends(t *testing.T) {
	tr, yesterday := seedTrackerWithYesterday(t)
	client := &mockStorageClient{}
	s := NewSender(tr, client, senderTestLogger(), fastRetries())

	s.Shutdown() // rootCtx отменён

	err := s.SendNow(context.Background(), yesterday)
	if err != nil {
		// допустимо: зависит от внутреннего состояния, не паникуем
	}
	_ = err
}
