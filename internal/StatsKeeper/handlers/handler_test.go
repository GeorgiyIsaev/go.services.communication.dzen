package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.services.communication.dzen/internal/StatsKeeper/service"
)

func newTrackerAt(now time.Time) *service.Tracker {
	tr := service.NewTracker()
	tr.SetNowFunc(func() time.Time { return now })
	return tr
}

func decodeStats(t *testing.T, body []byte) StatsResponse {
	t.Helper()
	var resp StatsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, string(body))
	}
	return resp
}

func TestStatsHandler_Success(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	tr.AddClickAt(1, 10, at)
	tr.AddClickAt(1, 11, at)
	tr.AddClickAt(1, 11, at) // дубль

	req := httptest.NewRequest(http.MethodGet, "/stats?author_ids=1,2", nil)
	rr := httptest.NewRecorder()

	StatsHandler(tr).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q, want application/json", ct)
	}

	resp := decodeStats(t, rr.Body.Bytes())
	if len(resp.Stats) != 2 {
		t.Fatalf("got %d stats, want 2", len(resp.Stats))
	}
	if resp.Stats[0].AuthorID != 1 || resp.Stats[0].Count != 2 {
		t.Fatalf("author 1: %#v, want {1, 2}", resp.Stats[0])
	}
	if resp.Stats[1].AuthorID != 2 || resp.Stats[1].Count != 0 {
		t.Fatalf("author 2: %#v, want {2, 0}", resp.Stats[1])
	}
}

func TestStatsHandler_PreservesOrder(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))

	req := httptest.NewRequest(http.MethodGet, "/stats?author_ids=3,1,2", nil)
	rr := httptest.NewRecorder()
	StatsHandler(tr).ServeHTTP(rr, req)

	resp := decodeStats(t, rr.Body.Bytes())
	got := []int64{resp.Stats[0].AuthorID, resp.Stats[1].AuthorID, resp.Stats[2].AuthorID}
	want := []int64{3, 1, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок нарушен: got %v, want %v", got, want)
		}
	}
}

func TestStatsHandler_MissingAuthorIDs(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Now())

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rr := httptest.NewRecorder()
	StatsHandler(tr).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", rr.Code)
	}
}

func TestStatsHandler_InvalidAuthorID(t *testing.T) {
	t.Parallel()

	cases := []string{
		"abc",
		"1,abc",
		"1,,2",
		"1,2,",
		",1",
		"1;2",
	}

	for _, param := range cases {
		param := param
		t.Run(param, func(t *testing.T) {
			t.Parallel()
			tr := newTrackerAt(time.Now())
			req := httptest.NewRequest(http.MethodGet, "/stats?author_ids="+param, nil)
			rr := httptest.NewRecorder()
			StatsHandler(tr).ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("param=%q code=%d, want 400", param, rr.Code)
			}
		})
	}
}

func TestStatsHandler_Whitespace(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))
	at := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	tr.AddClickAt(1, 10, at)

	// пробелы вокруг id
	req := httptest.NewRequest(http.MethodGet, "/stats?author_ids=+1+%2C+2+", nil)
	rr := httptest.NewRecorder()
	StatsHandler(tr).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s, want 200", rr.Code, rr.Body.String())
	}

	resp := decodeStats(t, rr.Body.Bytes())
	if len(resp.Stats) != 2 || resp.Stats[0].AuthorID != 1 || resp.Stats[1].AuthorID != 2 {
		t.Fatalf("некорректный ответ: %#v", resp.Stats)
	}
	if resp.Stats[0].Count != 1 {
		t.Fatalf("author 1 count=%d, want 1", resp.Stats[0].Count)
	}
}

func TestStatsHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Now())

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		m := m
		t.Run(m, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(m, "/stats?author_ids=1", strings.NewReader(""))
			rr := httptest.NewRecorder()
			StatsHandler(tr).ServeHTTP(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Fatalf("method=%s code=%d, want 405", m, rr.Code)
			}
		})
	}
}

func TestStatsHandler_ResponseIsValidJSON(t *testing.T) {
	t.Parallel()

	tr := newTrackerAt(time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC))
	req := httptest.NewRequest(http.MethodGet, "/stats?author_ids=1", nil)
	rr := httptest.NewRecorder()
	StatsHandler(tr).ServeHTTP(rr, req)

	if !json.Valid(rr.Body.Bytes()) {
		t.Fatalf("тело ответа не валидный JSON: %s", rr.Body.String())
	}

	// Проверим именно структуру полей — важно для контракта API.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["stats"]; !ok {
		t.Fatalf("в ответе нет поля stats: %s", rr.Body.String())
	}
}
