package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.services.communication.dzen/app/ClickImitator/internal/domain"
	"go.services.communication.dzen/app/ClickImitator/pkg/config"
)

var testSenderCfg = config.Sender{MaxRetries: 1, SendTimeout: time.Second}

func TestScenarioStrategy_Name(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, nil, discardLogger())
	if got := svc.Name(); got != "scenario" {
		t.Fatalf("Name() = %q, want %q", got, "scenario")
	}
}

func TestScenarioStrategy_TotalUsers(t *testing.T) {
	svc := NewScenarioStrategy(config.Scenario{}, []domain.Scenario{
		{UserID: 1}, {UserID: 2}, {UserID: 3},
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

func TestScenarioStrategy_Run(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10, 11}},
		{UserID: 2, Authors: []int64{20}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())
	sender := &mockSender{}

	if err := svc.Run(context.Background(), sender, testSenderCfg); err != nil {
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

	if err := svc.Run(context.Background(), sender, testSenderCfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0", sender.count())
	}
}

// Отменённый контекст — graceful abort, не ошибка.
func TestScenarioStrategy_Run_ContextCancelled(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10, 11, 12}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sender := &mockSender{}
	if err := svc.Run(ctx, sender, testSenderCfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("calls = %d, want 0 (ctx cancelled)", sender.count())
	}
}

// Исчерпанные retry теперь агрегируются и возвращаются как ошибка.
func TestScenarioStrategy_Run_SendError(t *testing.T) {
	scenarios := []domain.Scenario{
		{UserID: 1, Authors: []int64{10}},
	}
	svc := NewScenarioStrategy(config.Scenario{DelayBetweenReadsSec: 0}, scenarios, discardLogger())
	sender := &mockSender{errs: []error{errors.New("send failed")}}

	err := svc.Run(context.Background(), sender, testSenderCfg)
	if err == nil {
		t.Fatal("expected error from Run when send fails, got nil")
	}
	if sender.count() != 1 {
		t.Fatalf("calls = %d, want 1", sender.count())
	}
}
