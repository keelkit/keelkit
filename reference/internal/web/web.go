// Package web — общая оболочка HTML-страниц: layout, страницы ошибок, рендер templ-компонентов.
//
// Пакет не знает о типах контента. Данные для шапки (название сайта, меню) собирает cmd/site
// и кладёт в контекст запроса через WithSite; layout читает их через SiteFrom.
package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// Site — данные сайта для layout.
type Site struct {
	Name        string
	Description string
	Nav         []Link // пункты меню в шапке
}

// Link — пункт меню.
type Link struct {
	Title   string
	URL     string
	Current bool // ссылка ведёт на текущую страницу (aria-current)
}

type siteKey struct{}

// SiteFrom возвращает данные сайта из контекста запроса (пустые, если WithSite не применялся).
func SiteFrom(ctx context.Context) Site {
	s, _ := ctx.Value(siteKey{}).(Site)
	return s
}

// WithSite — middleware для HTML-маршрутов: загружает данные сайта и кладёт их в контекст.
// Ошибка загрузки — страница 500.
func WithSite(load func(context.Context) (Site, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			site, err := load(r.Context())
			if err != nil {
				ServerError(w, r, err)
				return
			}
			nav := make([]Link, len(site.Nav))
			for i, l := range site.Nav {
				l.Current = l.URL == r.URL.Path
				nav[i] = l
			}
			site.Nav = nav
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), siteKey{}, site)))
		})
	}
}

// Render отрисовывает компонент в буфер и отдаёт его со статусом status.
// Ошибка рендера не оставляет полуотрисованную страницу: клиент получает 500.
func Render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		slog.Error("render", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// NotFound — оформленная страница 404. Подходит как http.HandlerFunc для маршрута по умолчанию.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Render(w, r, http.StatusNotFound, NotFoundPage())
}

// ServerError логирует ошибку и отдаёт оформленную страницу 500 без подробностей.
func ServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	Render(w, r, http.StatusInternalServerError, ErrorPage())
}

// pageTitle — содержимое <title>: «Заголовок — Сайт» или только название сайта.
func pageTitle(title, siteName string) string {
	if title == "" || title == siteName {
		return siteName
	}
	if siteName == "" {
		return title
	}
	return title + " — " + siteName
}
