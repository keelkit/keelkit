package posts

import (
	"strconv"
	"time"
)

// Дата публикации хранится в UTC (RFC 3339, dbLayout): такие строки сравниваются в SQL как время.
// В админке (input type="datetime-local") и на сайте она показывается в часовом поясе сервера (time.Local,
// переменная окружения TZ).
const (
	dbLayout    = "2006-01-02T15:04:05Z"
	inputLayout = "2006-01-02T15:04"
)

// dbTime — время для колонки publish_date и для сравнения с ней.
func dbTime(t time.Time) string { return t.UTC().Format(dbLayout) }

// nowDB — текущее время в формате колонки: публичные запросы показывают посты с publish_date <= nowDB().
func nowDB() string { return dbTime(time.Now()) }

func parseDB(s string) (time.Time, bool) {
	t, err := time.Parse(dbLayout, s)
	return t, err == nil
}

// inputValue — дата из БД для поля datetime-local; пустая — пусто.
func inputValue(s string) string {
	t, ok := parseDB(s)
	if !ok {
		return ""
	}
	return t.In(time.Local).Format(inputLayout)
}

// parseInput — значение поля datetime-local (часовой пояс сервера) в формате колонки.
func parseInput(s string) (string, bool) {
	t, err := time.ParseInLocation(inputLayout, s, time.Local)
	if err != nil {
		return "", false
	}
	return dbTime(t), true
}

// months — названия месяцев в родительном падеже: «9 октября 2026».
var months = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}

// humanDate — дата публикации на сайте: «9 октября 2026».
func humanDate(s string) string {
	t, ok := parseDB(s)
	if !ok {
		return ""
	}
	t = t.In(time.Local)
	return strconv.Itoa(t.Day()) + " " + months[t.Month()-1] + " " + strconv.Itoa(t.Year())
}

// adminDate — дата в админке: «09.10.2026 12:00»; пустая — пусто.
func adminDate(s string) string {
	t, ok := parseDB(s)
	if !ok {
		return ""
	}
	return t.In(time.Local).Format("02.01.2006 15:04")
}

// isoDate — дата для атрибута datetime у <time>.
func isoDate(s string) string {
	t, ok := parseDB(s)
	if !ok {
		return ""
	}
	return t.In(time.Local).Format(time.RFC3339)
}

// scheduled — опубликованная копия появится на сайте позже.
func scheduled(publishDate string) bool { return publishDate > nowDB() }
