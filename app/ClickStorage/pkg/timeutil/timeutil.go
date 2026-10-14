package timeutil

import "time"

// DayStartUTC возвращает начало суток в UTC для переданного момента.
func DayStartUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// YesterdayUTC возвращает начало вчерашних суток в UTC.
func YesterdayUTC() time.Time {
	return DayStartUTC(time.Now().UTC().AddDate(0, 0, -1))
}
