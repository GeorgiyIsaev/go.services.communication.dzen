package domain

// AuthorStats — статистика кликов по одному автору за один день.
type AuthorStats struct {
	AuthorID int64 `json:"author_id"`
	Count    int64 `json:"count"`
}

// Author — справочная информация об авторе для дашборда.
type Author struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
}
