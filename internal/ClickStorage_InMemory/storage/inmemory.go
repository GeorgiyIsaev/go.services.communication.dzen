package storage

import (
	"context"
	"sync"
)

// Repository определяет контракт для работы с хранилищем данных.
type Repository interface {
	GetAuthorIDs(ctx context.Context) ([]int, error)
	AddAuthor(ctx context.Context, id int) error
	RemoveAuthor(ctx context.Context, id int) error
	SaveStats(ctx context.Context, stats map[int]int) error
}

// InMemoryRepo – потокобезопасное хранилище в памяти.
type InMemoryRepo struct {
	mu      sync.RWMutex
	authors []int
}

func NewInMemoryRepo() *InMemoryRepo {
	return &InMemoryRepo{authors: []int{}}
}

func (r *InMemoryRepo) GetAuthorIDs(ctx context.Context) ([]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]int, len(r.authors))
	copy(ids, r.authors)
	return ids, nil
}

func (r *InMemoryRepo) AddAuthor(ctx context.Context, id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.authors {
		if v == id {
			return nil
		}
	}
	r.authors = append(r.authors, id)
	return nil
}

func (r *InMemoryRepo) RemoveAuthor(ctx context.Context, id int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, v := range r.authors {
		if v == id {
			r.authors = append(r.authors[:i], r.authors[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *InMemoryRepo) SaveStats(ctx context.Context, stats map[int]int) error {
	// Заглушка для будущего подключения БД
	return nil
}
