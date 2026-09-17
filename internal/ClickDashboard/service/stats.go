package service

import (
	"database/sql"
	"fmt"
	"time"

	"go.services.communication.dzen/internal/ClickDashboard/models"
)

type StatsService struct {
	db *sql.DB
}

func NewStatsService(db *sql.DB) *StatsService {
	return &StatsService{db: db}
}

func (s *StatsService) GetStats(days, limit, offset int) (*models.StatsResponse, error) {
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	startDate := yesterday.AddDate(0, 0, -days+1)

	startDateStr := startDate.Format("2006-01-02")
	yesterdayStr := yesterday.Format("2006-01-02")

	// 1. Получить все даты, за которые есть записи в stats за период
	dateRows, err := s.db.Query(`
		SELECT DISTINCT date::date
		FROM stats
		WHERE date::date BETWEEN $1::date AND $2::date
		ORDER BY date::date DESC
	`, startDateStr, yesterdayStr)
	if err != nil {
		return nil, err
	}
	defer dateRows.Close()

	var dates []string
	for dateRows.Next() {
		var d time.Time
		if err := dateRows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d.UTC().Format("2006-01-02"))
	}
	if err := dateRows.Err(); err != nil {
		return nil, err
	}

	resp := &models.StatsResponse{
		Dates:   dates,
		Authors: []models.AuthorStats{},
		Limit:   limit,
		Offset:  offset,
	}

	// Общее количество авторов (для пагинации)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM authors`).Scan(&resp.Total); err != nil {
		return nil, err
	}

	// Если дат нет — нечего показывать, но total отдаём, чтобы фронт знал про пагинацию
	if len(dates) == 0 {
		return resp, nil
	}

	// 2. Авторы + клики одним запросом, с пагинацией
	rows, err := s.db.Query(`
		SELECT a.id, a.email, a.first_name, a.last_name, s.date::date, s.clicks
		FROM (
			SELECT id, email, first_name, last_name
			FROM authors
			ORDER BY id
			LIMIT $3 OFFSET $4
		) a
		LEFT JOIN stats s
			ON s.author_id = a.id
			AND s.date::date BETWEEN $1::date AND $2::date
		ORDER BY a.id, s.date DESC
	`, startDateStr, yesterdayStr, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	authorsMap := make(map[int]*models.Author)
	var authorsOrder []int
	clicksMap := make(map[int]map[string]int)

	for rows.Next() {
		var (
			id        int
			email     string
			firstName string
			lastName  string
			date      sql.NullTime
			clicks    sql.NullInt64
		)
		if err := rows.Scan(&id, &email, &firstName, &lastName, &date, &clicks); err != nil {
			return nil, err
		}

		if _, ok := authorsMap[id]; !ok {
			authorsMap[id] = &models.Author{
				ID:        id,
				Email:     email,
				FirstName: firstName,
				LastName:  lastName,
			}
			authorsOrder = append(authorsOrder, id)
		}

		// При LEFT JOIN строки без статистики приходят с NULL — их просто пропускаем
		if date.Valid && clicks.Valid {
			dateStr := date.Time.UTC().Format("2006-01-02")
			if _, ok := clicksMap[id]; !ok {
				clicksMap[id] = make(map[string]int)
			}
			clicksMap[id][dateStr] = int(clicks.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 3. Формируем ответ, сохраняя порядок авторов из запроса
	for _, id := range authorsOrder {
		author := authorsMap[id]
		authorStats := models.AuthorStats{
			ID:     author.ID,
			Name:   fmt.Sprintf("%s %s <%s>", author.FirstName, author.LastName, author.Email),
			Clicks: make([]int, len(dates)),
		}
		for i, d := range dates {
			if val, ok := clicksMap[author.ID][d]; ok {
				authorStats.Clicks[i] = val
			}
		}
		resp.Authors = append(resp.Authors, authorStats)
	}

	return resp, nil
}
