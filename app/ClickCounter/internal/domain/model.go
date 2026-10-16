package domain

// AuthorStat — агрегированный счётчик по одному автору за день.
// Формат совпадает с тем, что ожидает StorageGateway в POST /stats.
type AuthorStat struct {
	AuthorID int64 `json:"author_id"`
	Count    int64 `json:"count"`
}
