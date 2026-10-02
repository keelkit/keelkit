package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/settings"
	"github.com/keelkit/keelkit/reference/internal/web"
	"github.com/keelkit/keelkit/reference/static"
)

// defaultSiteName — название, пока в site_settings нет строки (до seed или настройки в админке).
const defaultSiteName = "Keelkit reference"

// newMux — все маршруты сайта. HTML-маршруты оборачиваются в html: данные шапки в контексте запроса.
// Маршруты админки — ещё и в adminOnly: защита от межсайтовых запросов (http.CrossOriginProtection),
// а всё, кроме входа, — в adminH.RequireUser (сессия и CSRF-токен).
func newMux(db *sql.DB, cfg config) *http.ServeMux {
	html := web.WithSite(siteLoader(db))
	pagesH := pages.NewHandler(db)
	adminH := admin.NewHandler(db, admin.Options{
		SecureCookies: cfg.CookieSecure,
		Counters:      adminCounters(db),
	})
	crossOrigin := http.NewCrossOriginProtection()
	adminOnly := func(h http.HandlerFunc) http.Handler { return crossOrigin.Handler(html(h)) }
	signedIn := func(h http.HandlerFunc) http.Handler { return adminOnly(adminH.RequireUser(h).ServeHTTP) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz(db))
	mux.Handle("GET /static/", static.Handler())

	mux.Handle("GET /{$}", html(http.HandlerFunc(pagesH.Home)))
	mux.Handle("GET /{slug}", html(http.HandlerFunc(pagesH.Show)))

	mux.Handle("GET /admin/login", adminOnly(adminH.LoginForm))
	mux.Handle("POST /admin/login", adminOnly(adminH.Login))
	mux.Handle("POST /admin/logout", signedIn(adminH.Logout))
	mux.Handle("GET /admin", signedIn(adminH.Dashboard))
	mux.Handle("GET /admin/{$}", http.RedirectHandler("/admin", http.StatusMovedPermanently))
	// Остальное под /admin/ — 404 админки, но только после входа: без сессии — редирект на вход.
	mux.Handle("/admin/", signedIn(adminH.NotFound))

	// Всё остальное — оформленная 404.
	mux.Handle("/", html(http.HandlerFunc(web.NotFound)))
	return mux
}

// siteLoader собирает данные для layout: название и описание сайта, меню из страниц (кроме главной).
func siteLoader(db *sql.DB) func(context.Context) (web.Site, error) {
	sq := settings.New(db)
	pq := pages.New(db)
	return func(ctx context.Context) (web.Site, error) {
		site := web.Site{Name: defaultSiteName}
		s, err := sq.GetSiteSettings(ctx)
		switch {
		case err == nil:
			site.Name, site.Description = s.Name, s.Description
		case !errors.Is(err, sql.ErrNoRows):
			return web.Site{}, fmt.Errorf("site settings: %w", err)
		}

		links, err := pq.ListPageLinks(ctx, pages.HomeSlug)
		if err != nil {
			return web.Site{}, fmt.Errorf("page links: %w", err)
		}
		for _, l := range links {
			site.Nav = append(site.Nav, web.Link{Title: l.Title, URL: "/" + url.PathEscape(l.Slug)})
		}
		return site, nil
	}
}

// adminCounters — счётчики контента для дашборда админки.
func adminCounters(db *sql.DB) func(context.Context) ([]admin.Counter, error) {
	pq := pages.New(db)
	return func(ctx context.Context) ([]admin.Counter, error) {
		n, err := pq.CountPages(ctx)
		if err != nil {
			return nil, fmt.Errorf("count pages: %w", err)
		}
		return []admin.Counter{{Label: "Страниц", Value: n}}, nil
	}
}

type healthResponse struct {
	Status  string                  `json:"status"`
	Version string                  `json:"version"`
	Schema  map[string]schemaHealth `json:"schema,omitempty"`
}

type schemaHealth struct {
	Version int64 `json:"version"`
	Pending int   `json:"pending"`
}

func handleHealthz(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{Status: "ok", Version: version}
		code := http.StatusOK
		schemas, err := database.Status(r.Context(), db, migrationSources)
		if err != nil {
			slog.Error("healthz: schema status", "err", err)
			resp.Status, code = "error", http.StatusServiceUnavailable
		} else {
			resp.Schema = make(map[string]schemaHealth, len(schemas))
			for _, s := range schemas {
				resp.Schema[s.Name] = schemaHealth{Version: s.Version, Pending: s.Pending}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
