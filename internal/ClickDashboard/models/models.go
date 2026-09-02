package models

type Author struct {
	ID        int    `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type StatsResponse struct {
	Dates   []string      `json:"dates"`
	Authors []AuthorStats `json:"authors"`
}

type AuthorStats struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Clicks []int  `json:"clicks"`
}
