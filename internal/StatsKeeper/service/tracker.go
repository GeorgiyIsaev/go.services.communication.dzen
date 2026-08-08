package service

import (
	"sync"
	"time"
)

// Tracker хранит клики по дням и обеспечивает потокобезопасный доступ.
type Tracker struct {
	mu           sync.RWMutex
	currentDay   string                       // день в формате "2006-01-02"
	previousDay  string                       // предыдущий день (вчера)
	currentData  map[int64]map[int64]struct{} // author -> user -> struct{}
	previousData map[int64]map[int64]struct{} // author -> user -> struct{}
	nowFunc      func() time.Time             // функция получения текущего времени (для тестов)
}

// NewTracker создаёт новый экземпляр Tracker.
func NewTracker() *Tracker {
	return &Tracker{
		currentData:  make(map[int64]map[int64]struct{}),
		previousData: make(map[int64]map[int64]struct{}),
		nowFunc:      time.Now,
	}
}

// dayKey возвращает строку дня для переданного времени в UTC.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// SetNowFunc устанавливает функцию получения текущего времени (для тестов).
func (t *Tracker) SetNowFunc(fn func() time.Time) {
	t.nowFunc = fn
}

// rotateDay проверяет, не наступил ли новый день, и при необходимости выполняет ротацию данных.
// Метод вызывается перед каждым добавлением или чтением статистики.
func (t *Tracker) rotateDay(now time.Time) {
	newDay := dayKey(now)
	if newDay == t.currentDay {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Повторная проверка после блокировки
	if newDay == t.currentDay {
		return
	}

	// Первый запуск – инициализируем
	if t.currentDay == "" {
		t.currentDay = newDay
		t.currentData = make(map[int64]map[int64]struct{})
		t.previousData = make(map[int64]map[int64]struct{})
		return
	}

	// Вычисляем разницу в днях
	currentTime, _ := time.Parse("2006-01-02", t.currentDay)
	newTime, _ := time.Parse("2006-01-02", newDay)
	diff := int(newTime.Sub(currentTime).Hours() / 24)

	if diff == 1 {
		// Обычный переход на следующий день: текущий становится предыдущим
		t.previousData = t.currentData
		t.previousDay = t.currentDay
	} else if diff > 1 {
		// Пропущен один или более дней – данные за вчера отсутствуют
		t.previousData = make(map[int64]map[int64]struct{})
		t.previousDay = ""
	}

	// Создаём новую карту для текущего дня
	t.currentData = make(map[int64]map[int64]struct{})
	t.currentDay = newDay
}

// AddClick добавляет клик пользователя по автору в текущий день.
func (t *Tracker) AddClick(authorID, userID int64) {
	now := t.nowFunc()
	t.rotateDay(now)

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.currentData[authorID]; !ok {
		t.currentData[authorID] = make(map[int64]struct{})
	}
	t.currentData[authorID][userID] = struct{}{}
}

// GetYesterdayStats возвращает количество уникальных пользователей для каждого запрошенного автора
// за предыдущий календарный день (вчера). Для авторов без данных возвращается 0.
func (t *Tracker) GetYesterdayStats(authorIDs []int64) map[int64]int {
	now := t.nowFunc()
	t.rotateDay(now)

	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[int64]int, len(authorIDs))

	if t.previousDay == "" {
		// Данных за предыдущий день нет
		for _, id := range authorIDs {
			result[id] = 0
		}
		return result
	}

	for _, id := range authorIDs {
		users, ok := t.previousData[id]
		if !ok {
			result[id] = 0
			continue
		}
		result[id] = len(users)
	}
	return result
}
