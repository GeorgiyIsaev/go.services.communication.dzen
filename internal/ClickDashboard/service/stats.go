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

func (s *StatsService) GetStats(days int) (*models.StatsResponse, error) {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	startDate := yesterday.AddDate(0, 0, -days+1)

	// 1. Получить все даты, за которые есть записи в stats за период
	rows, err := s.db.Query(`
		SELECT DISTINCT date
		FROM stats
		WHERE date BETWEEN $1 AND $2
		ORDER BY date DESC
	`, startDate, yesterday)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d.Format("2006-01-02"))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(dates) == 0 {
		return &models.StatsResponse{Dates: []string{}, Authors: []models.AuthorStats{}}, nil
	}

	// 2. Получить всех авторов
	authorRows, err := s.db.Query(`
		SELECT id, email, first_name, last_name
		FROM authors
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer authorRows.Close()

	var authors []models.Author
	for authorRows.Next() {
		var a models.Author
		if err := authorRows.Scan(&a.ID, &a.Email, &a.FirstName, &a.LastName); err != nil {
			return nil, err
		}
		authors = append(authors, a)
	}
	if err := authorRows.Err(); err != nil {
		return nil, err
	}

	// 3. Получить все клики за период (используем BETWEEN, без передачи массива дат)
	statsRows, err := s.db.Query(`
		SELECT author_id, date, clicks
		FROM stats
		WHERE date BETWEEN $1 AND $2
	`, startDate, yesterday)
	if err != nil {
		return nil, err
	}
	defer statsRows.Close()

	// Мапа: authorID -> map[date]clicks
	clicksMap := make(map[int]map[string]int)
	for statsRows.Next() {
		var authorID int
		var date time.Time
		var clicks int
		if err := statsRows.Scan(&authorID, &date, &clicks); err != nil {
			return nil, err
		}
		dateStr := date.Format("2006-01-02")
		if _, ok := clicksMap[authorID]; !ok {
			clicksMap[authorID] = make(map[string]int)
		}
		clicksMap[authorID][dateStr] = clicks
	}
	if err := statsRows.Err(); err != nil {
		return nil, err
	}

	// 4. Формируем ответ
	resp := &models.StatsResponse{
		Dates: dates,
	}
	for _, author := range authors {
		authorStats := models.AuthorStats{
			ID:     author.ID,
			Name:   fmt.Sprintf("%s %s <%s>", author.FirstName, author.LastName, author.Email),
			Clicks: make([]int, len(dates)),
		}
		for i, d := range dates {
			if val, ok := clicksMap[author.ID][d]; ok {
				authorStats.Clicks[i] = val
			} else {
				authorStats.Clicks[i] = 0
			}
		}
		resp.Authors = append(resp.Authors, authorStats)
	}

	return resp, nil
}
