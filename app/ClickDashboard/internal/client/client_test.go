package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/pkg/reqid"
)

func TestGetAuthors_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/authors" {
			t.Errorf("path = %s, want /authors", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authors":[{"id":1,"first_name":"John","last_name":"Doe","email":"j@d.com"}]}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	authors, err := c.GetAuthors(context.Background())
	if err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if len(authors) != 1 || authors[0].ID != 1 || authors[0].FirstName != "John" {
		t.Fatalf("unexpected authors: %+v", authors)
	}
}

func TestGetStatsByDate_PassesDateParam(t *testing.T) {
	var gotDate string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDate = r.URL.Query().Get("date")
		_, _ = w.Write([]byte(`{"date":"2024-01-15T00:00:00Z","stats":[{"author_id":7,"count":42}]}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	date := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	stats, err := c.GetStatsByDate(context.Background(), date)
	if err != nil {
		t.Fatalf("GetStatsByDate: %v", err)
	}
	if gotDate != "2024-01-15" {
		t.Errorf("date param = %q, want 2024-01-15", gotDate)
	}
	if len(stats) != 1 || stats[0].AuthorID != 7 || stats[0].Count != 42 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestDoJSON_PropagatesRequestID(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get(reqid.HeaderName)
		_, _ = w.Write([]byte(`{"authors":[]}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	ctx := reqid.WithContext(context.Background(), "test-req-id-123")
	if _, err := c.GetAuthors(ctx); err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if gotID != "test-req-id-123" {
		t.Errorf("X-Request-ID = %q, want test-req-id-123", gotID)
	}
}

func TestDoJSON_NoRequestID_HeaderNotSet(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header[reqid.HeaderName]
		_, _ = w.Write([]byte(`{"authors":[]}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	if _, err := c.GetAuthors(context.Background()); err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if present {
		t.Error("X-Request-ID should not be set when ctx has no request_id")
	}
}

func TestDoJSON_5xx_DoesNotLeakBody(t *testing.T) {
	const secret = "SECRET_TABLE_NAME"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secret))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	_, err := c.GetAuthors(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUpstreamBadResponse) {
		t.Errorf("got %v, want ErrUpstreamBadResponse", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks upstream body: %v", err)
	}
}

func TestDoJSON_4xx_IncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad date"}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	_, err := c.GetStatsByDate(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUpstreamBadResponse) {
		t.Errorf("got %v, want ErrUpstreamBadResponse", err)
	}
	if !strings.Contains(err.Error(), "bad date") {
		t.Errorf("4xx body should be visible: %v", err)
	}
}

func TestDoJSON_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not a json`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 5*time.Second)
	_, err := c.GetAuthors(context.Background())
	if !errors.Is(err, ErrUpstreamBadResponse) {
		t.Errorf("got %v, want ErrUpstreamBadResponse", err)
	}
}

func TestDoJSON_NetworkError(t *testing.T) {
	// Поднимаем и сразу закрываем сервер, чтобы гарантированно получить отказ соединения.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewStorageGatewayClient(url, time.Second)
	_, err := c.GetAuthors(context.Background())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("got %v, want ErrUpstreamUnavailable", err)
	}
}

func TestDoJSON_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"authors":[]}`))
	}))
	defer srv.Close()

	c := NewStorageGatewayClient(srv.URL, 20*time.Millisecond)
	_, err := c.GetAuthors(context.Background())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("got %v, want ErrUpstreamUnavailable", err)
	}
}
