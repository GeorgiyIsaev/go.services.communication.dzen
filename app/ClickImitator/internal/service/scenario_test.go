package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

func TestScenarioStrategy_Name(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, nil, discardLogger())
	if got := svc.Name(); got != "scenario" {
		t.Fatalf("Name() = %q, want %q", got, "scenario")
	}
}

func TestScenarioStrategy_TotalUsers(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, []domain.Scenario{
		{UserID: 1},
		{UserID: 2},
		{UserID: 3},
	}, discardLogger())

	if got := svc.TotalUsers(); got != 3 {
		t.Fatalf("TotalUsers() = %d, want 3", got)
	}
}

func TestScenarioStrategy_TotalUsers_Empty(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, nil, discardLogger())
	if got := svc.TotalUsers(); got != 0 {
		t.Fatalf("TotalUsers() = %d, want 0", got)
	}
}

// DelayBetweenReadsSec = 0 делает Run быстрым и детерминированным по числу вызовов.
func TestScenarioStrategy_Run(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10, 11}},
		{UserID: 2, Authors: []int64{20}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())
	sender := &mockSender{}

	if err := svc.Run(context.Background(), sender, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sender.count() != 3 {
		t.Fatalf("calls = %d, want 3", sender.count())
	}

	seen := make(map[string]bool, 3)
	for _, c := range sender.snapshot() {
		key := fmt.Sprintf("%d->%d", c.UserID, c.AuthorID)
		seen[key] = true
		if c.Timestamp.IsZero() {
			t.Errorf("timestamp is zero for %s", key)
		}
	}
	for _, want := range []string{"1->10", "1->11", "2->20"} {
		if !seen[want] {
			t.Errorf("missing call %s (got %v)", want, seen)
		}
	}
}

func TestScenarioStrategy_Run_Empty(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, nil, discardLogger())
	sender := &mockSender{}

	if err := svc.Run(context.Background(), sender, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0", sender.count())
	}
}

// Отменённый контекст: sendWithRetry проверяет ctx.Err() в начале и
// не должен отправить ни одного клика.
func TestScenarioStrategy_Run_ContextCancelled(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10, 11, 12}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sender := &mockSender{}
	_ = svc.Run(ctx, sender, 1)

	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0 (ctx cancelled)", sender.count())
	}
}

// Ошибки отправки не возвращаются наружу — Run не должен падать.
func TestScenarioStrategy_Run_SendError(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())
	sender := &mockSender{errs: []error{errors.New("send failed")}}

	if err := svc.Run(context.Background(), sender, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}
