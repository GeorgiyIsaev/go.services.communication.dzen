package service

import (
	"sync"
	"time"
)

// Tracker хранит клики по дням и обеспечивает потокобезопасный доступ.
type Tracker struct {
	mu      sync.RWMutex
	days    map[string]map[int64]map[int64]struct{} // day -> author -> user -> struct{}
	nowFunc func() time.Time
}

// NewTracker создаёт новый экземпляр Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		days:    make(map[string]map[int64]map[int64]struct{}),
		nowFunc: time.Now,
	}
}

// dayKey возвращает строку дня для переданного времени в UTC.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// SetNowFunc устанавливает функцию получения текущего времени (для тестов).
func (t *Tracker) SetNowFunc(fn func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nowFunc = fn
}

// AddClick добавляет клик пользователя по автору в текущий день.
func (t *Tracker) AddClick(authorID, userID int64) {
	t.AddClickAt(authorID, userID, time.Now())
}

// AddClickAt добавляет клик на конкретный момент времени.
func (t *Tracker) AddClickAt(authorID, userID int64, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFunc()
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

	// Чистим всё, что старше вчера
	for d := range t.days {
		if d != today && d != yesterday {
			delete(t.days, d)
		}
	}
}

// GetYesterdayStats возвращает количество уникальных пользователей для каждого запрошенного автора
// за предыдущий календарный день (вчера). Для авторов без данных возвращается 0.
func (t *Tracker) GetYesterdayStats(authorIDs []int64) map[int64]int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	yesterday := dayKey(t.nowFunc().AddDate(0, 0, -1))
	dayData := t.days[yesterday]

	result := make(map[int64]int, len(authorIDs))
	for _, id := range authorIDs {
		if users, ok := dayData[id]; ok {
			result[id] = len(users)
		} else {
			result[id] = 0
		}
	}
	return result
}
