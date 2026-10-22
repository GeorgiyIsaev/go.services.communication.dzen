package service

import (
	"sync"
	"time"

	"go.services.communication.dzen/app/ClickCounter/internal/domain"
)

// Tracker хранит клики по дням (только сегодня и вчера)
type Tracker struct {
	mu      sync.RWMutex
	days    map[string]map[int64]map[int64]struct{}
	nowFunc func() time.Time
}

func NewTracker() *Tracker {
	return &Tracker{
		days:    make(map[string]map[int64]map[int64]struct{}),
		nowFunc: time.Now,
	}
}

// SetNowFunc — для тестов.
func (t *Tracker) SetNowFunc(fn func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nowFunc = fn
}

func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// AddClick — добавить клик «сейчас».
func (t *Tracker) AddClick(authorID, userID int64) {
	t.AddClickAt(authorID, userID, time.Now().UTC())
}

// AddClickAt добавляет клик на конкретный момент времени.
func (t *Tracker) AddClickAt(authorID, userID int64, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFunc().UTC()
	today := dayKey(now)
	yesterday := dayKey(now.AddDate(0, 0, -1))
	msgDay := dayKey(at)

	var target string
	switch msgDay {
	case today, yesterday:
		target = msgDay
	default:
		return
	}

	dayData, ok := t.days[target]
	if !ok {
		dayData = make(map[int64]map[int64]struct{})
		t.days[target] = dayData
	}
	users, ok := dayData[authorID]
	if !ok {
		users = make(map[int64]struct{})
		dayData[authorID] = users
	}
	users[userID] = struct{}{}
}

// Cleanup удаляет все дни, кроме сегодняшнего и вчерашнего (UTC).
func (t *Tracker) Cleanup(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now = now.UTC()
	today := dayKey(now)
	yesterday := dayKey(now.AddDate(0, 0, -1))

	for d := range t.days {
		if d != today && d != yesterday {
			delete(t.days, d)
		}
	}
}

// GetByDate возвращает агрегированные данные за указанный день (UTC).
// Авторы без кликов не возвращаются.
func (t *Tracker) GetByDate(date time.Time) []domain.AuthorStat {
	t.mu.RLock()
	defer t.mu.RUnlock()

	dayData := t.days[dayKey(date)]
	result := make([]domain.AuthorStat, 0, len(dayData))
	for authorID, users := range dayData {
		result = append(result, domain.AuthorStat{
			AuthorID: authorID,
			Count:    int64(len(users)),
		})
	}
	return result
}
