package domain

import (
	"context"
	"fmt"
	"time"
)

// ClickMessage — то, что уходит в Kafka.
type ClickMessage struct {
	UserID    int64     `json:"user_id"`
	AuthorID  int64     `json:"author_id"`
	Timestamp time.Time `json:"timestamp"`
}

// ClickSender — контракт на отправку клика (порт).
// Реализация — в internal/client.
type ClickSender interface {
	Send(ctx context.Context, req ClickMessage) error
}

// Scenario описывает сценарий чтений одного пользователя.
type Scenario struct {
	UserID  int64
	Authors []int64
}

func (s Scenario) Validate() error {
	if s.UserID == 0 {
		return fmt.Errorf("не задан user_id")
	}
	if len(s.Authors) == 0 {
		return fmt.Errorf("для user_id=%d не задан список authors", s.UserID)
	}
	return nil
}
