package domain

// Author — справочная информация об авторе.
type Author struct {
	ID        int64
	FirstName string
	LastName  string
	Email     string
}

// AuthorStats — статистика по одному автору за один день.
type AuthorStats struct {
	AuthorID int64
	Count    int64
}

// AuthorRow — строка таблицы для фронта: один автор и его клики по дням.
type AuthorRow struct {
	ID        int64   `json:"id"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	Email     string  `json:"email"`
	Clicks    []int64 `json:"clicks"`
}

// StatsMatrix — то, что ClickDashboard отдаёт фронту по GET /stats.
type StatsMatrix struct {
	Dates   []string    `json:"dates"`    // даты по убыванию (свежая слева), формат 2006-01-02
	Authors []AuthorRow `json:"authors"`  // авторы по возрастанию id
	MaxDays int         `json:"max_days"` // дублируем лимит, чтобы фронт знал
}
