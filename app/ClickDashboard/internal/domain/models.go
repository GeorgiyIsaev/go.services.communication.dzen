package domain

import "time"

// Author — справочная информация об авторе (то, что отдаёт GET /authors StorageGateway).
type Author struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}

// AuthorStats — статистика по одному автору за один день
// (то, что отдаёт GET /dashboard StorageGateway).
type AuthorStats struct {
	AuthorID int64 `json:"author_id"`
	Count    int64 `json:"count"`
}

// AuthorsResponse — обёртка ответа GET /authors.
type AuthorsResponse struct {
	Authors []Author `json:"authors"`
}

// DashboardResponse — обёртка ответа GET /dashboard.
type DashboardResponse struct {
	Date  time.Time     `json:"date"`
	Stats []AuthorStats `json:"stats"`
}

// AuthorRow — строка таблицы для фронта: один автор и его клики по дням.
type AuthorRow struct {
	ID        int64   `json:"id"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	Email     string  `json:"email"`
	Clicks    []int64 `json:"clicks"` // выровнено по StatsMatrix.Dates
}

// StatsMatrix — то, что ClickDashboard отдаёт фронту по GET /stats.
type StatsMatrix struct {
	Dates   []string    `json:"dates"`    // даты по убыванию (свежая слева), формат 2006-01-02
	Authors []AuthorRow `json:"authors"`  // авторы по возрастанию id
	MaxDays int         `json:"max_days"` // дублируем лимит, чтобы фронт знал
}
