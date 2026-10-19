package service

import (
	"sync/atomic"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
)

// Snapshot — проекция.
type Snapshot struct {
	Dates   []string           // по убыванию (свежая слева)
	Authors []domain.AuthorRow // отсортированы по ID, Clicks выровнены с Dates
	MaxDays int                // исходный лимит из конфига (для ответа фронту)
	BuiltAt time.Time
}

// Slice возвращает view на первые days дней.
func (s *Snapshot) Slice(days int) *domain.StatsMatrix {
	if days < 1 {
		days = 1
	}
	if days > len(s.Dates) {
		days = len(s.Dates)
	}

	rows := make([]domain.AuthorRow, len(s.Authors))
	for i, a := range s.Authors {
		rows[i] = a
		rows[i].Clicks = a.Clicks[:days:days]
	}

	return &domain.StatsMatrix{
		Dates:   s.Dates[:days:days],
		Authors: rows,
		MaxDays: s.MaxDays,
	}
}

// Store — контейнер текущего снапшота.
type Store struct {
	snap atomic.Pointer[Snapshot]
}

func NewStore() *Store { return &Store{} }

// Get возвращает текущий снапшот (nil, если Refresh ещё не отработал).
func (s *Store) Get() *Snapshot { return s.snap.Load() }

// Publish атомарно подменяет снапшот.
func (s *Store) Publish(sn *Snapshot) { s.snap.Store(sn) }
