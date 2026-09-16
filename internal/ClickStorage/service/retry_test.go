package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestShouldRetryStatus(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{http.StatusOK, false},
		{http.StatusCreated, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusForbidden, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{599, true},
	}
	for _, tc := range cases {
		if got := shouldRetryStatus(tc.code); got != tc.want {
			t.Errorf("status %d: want %v, got %v", tc.code, tc.want, got)
		}
	}
}

func TestDoWithRetry_RetriesOn5xxThenSucceeds(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"stats":[]}`))
	}))
	defer srv.Close()

	svc := New(nil, srv.URL, 100, 3)
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := svc.doWithRetry(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Fatalf("want 3 attempts, got %d", attempts)
	}
}

func TestDoWithRetry_DoesNotRetryOn4xx(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	svc := New(nil, srv.URL, 100, 3)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	resp, err := svc.doWithRetry(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 passthrough, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("4xx must not retry, attempts=%d", attempts)
	}
}

func TestDoWithRetry_ExhaustsRetries(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	svc := New(nil, srv.URL, 100, 2)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	_, err := svc.doWithRetry(req)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// maxRetries=2 → 3 попытки всего
	if attempts != 3 {
		t.Fatalf("want 3 attempts, got %d", attempts)
	}
}

func TestDoWithRetry_ZeroRetries(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := New(nil, srv.URL, 100, 0)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)

	_, err := svc.doWithRetry(req)
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("with maxRetries=0 must be single attempt, got %d", attempts)
	}
}

func TestDoWithRetry_StopsOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	svc := New(nil, srv.URL, 100, 10)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = svc.doWithRetry(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error")
	}
	// Без проверки ctx.Done в backoff-селекте тест шёл бы все 10 попыток
	// и занял бы много секунд. С проверкой — не больше пары сотен мс.
	if elapsed > 2*time.Second {
		t.Fatalf("should abort quickly on cancel, took %v", elapsed)
	}
}
