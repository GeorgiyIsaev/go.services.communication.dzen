package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// --- discoverPartitions: fallback по брокерам ---

func partitions(ids ...int) []kafka.Partition {
	out := make([]kafka.Partition, len(ids))
	for i, id := range ids {
		out[i] = kafka.Partition{ID: id}
	}
	return out
}

// Если первый брокер недоступен — берём следующий, а не падаем.
func TestDiscoverPartitionsFrom_FallsBackToNextBroker(t *testing.T) {
	t.Parallel()

	var tried []string
	got, err := discoverPartitionsFrom(context.Background(),
		[]string{"kafka1:19092", "kafka2:19093", "kafka3:19094"},
		func(_ context.Context, broker string) ([]kafka.Partition, error) {
			tried = append(tried, broker)
			if broker != "kafka3:19094" {
				return nil, errors.New("connection refused")
			}
			return partitions(0, 1, 2), nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("partitions = %v, want [0 1 2]", got)
	}
	want := []string{"kafka1:19092", "kafka2:19093", "kafka3:19094"}
	if len(tried) != 3 || tried[0] != want[0] || tried[1] != want[1] || tried[2] != want[2] {
		t.Fatalf("tried = %v, want %v", tried, want)
	}
}

// Первый же доступный брокер останавливает перебор.
func TestDiscoverPartitionsFrom_StopsAtFirstSuccess(t *testing.T) {
	t.Parallel()

	var tried int
	got, err := discoverPartitionsFrom(context.Background(),
		[]string{"kafka1:19092", "kafka2:19093"},
		func(context.Context, string) ([]kafka.Partition, error) {
			tried++
			return partitions(0, 1), nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tried != 1 {
		t.Errorf("tried = %d brokers, want 1", tried)
	}
	if len(got) != 2 {
		t.Errorf("partitions = %v, want 2 ids", got)
	}
}

// Все брокеры недоступны → ошибка со списком причин.
func TestDiscoverPartitionsFrom_AllBrokersFail(t *testing.T) {
	t.Parallel()

	_, err := discoverPartitionsFrom(context.Background(),
		[]string{"kafka1:19092", "kafka2:19093"},
		func(_ context.Context, broker string) ([]kafka.Partition, error) {
			return nil, errors.New("connection refused")
		})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "kafka1:19092") || !strings.Contains(err.Error(), "kafka2:19093") {
		t.Errorf("error should mention all brokers, got: %v", err)
	}
}

func TestDiscoverPartitionsFrom_EmptyBrokers(t *testing.T) {
	t.Parallel()

	_, err := discoverPartitionsFrom(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty broker list")
	}
}

func TestDiscoverPartitionsFrom_CanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := discoverPartitionsFrom(ctx, []string{"kafka1:19092"},
		func(context.Context, string) ([]kafka.Partition, error) {
			t.Error("fn must not be called with canceled ctx")
			return nil, nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// --- Контракт Kafka: дату определяет имитатор ---
func TestClickMessage_MatchesImitatorContract(t *testing.T) {
	t.Parallel()

	imitatorPayload := `{"user_id":42,"author_id":7,"timestamp":"2024-06-15T10:30:00Z"}`
	var m ClickMessage
	if err := json.Unmarshal([]byte(imitatorPayload), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.UserID != 42 || m.AuthorID != 7 {
		t.Errorf("ids = user %d, author %d; want 42/7", m.UserID, m.AuthorID)
	}
	want := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	if !m.Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", m.Timestamp, want)
	}
}

// Сообщение без timestamp разбирается, но поле остаётся нулевым —
// это нарушение контракта: consumePartition отбрасывает его (eventTime → false).
func TestClickMessage_MissingTimestampParses(t *testing.T) {
	t.Parallel()

	var m ClickMessage
	if err := json.Unmarshal([]byte(`{"user_id":1,"author_id":2}`), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.UserID != 1 || m.AuthorID != 2 {
		t.Errorf("ids = %d/%d, want 1/2", m.UserID, m.AuthorID)
	}
	if !m.Timestamp.IsZero() {
		t.Errorf("timestamp = %v, want zero", m.Timestamp)
	}
}

// Строгий контракт: есть timestamp → берём его; нет → (zero, false),
// сообщение отбрасывается. Fallback'ов на время брокера/«сейчас» нет.
func TestEventTime(t *testing.T) {
	t.Parallel()

	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)

	t.Run("timestamp из сообщения → момент клика", func(t *testing.T) {
		t.Parallel()
		got, ok := eventTime(ClickMessage{Timestamp: ts})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if !got.Equal(ts) {
			t.Errorf("got %v, want %v", got, ts)
		}
	})

	t.Run("нет timestamp → нарушение контракта", func(t *testing.T) {
		t.Parallel()
		got, ok := eventTime(ClickMessage{})
		if ok {
			t.Fatal("ok = true, want false (contract violation)")
		}
		if !got.IsZero() {
			t.Errorf("got %v, want zero", got)
		}
	})
}
