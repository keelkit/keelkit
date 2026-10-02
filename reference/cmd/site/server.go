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

	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/settings"
	"github.com/keelkit/keelkit/reference/internal/web"
	"github.com/keelkit/keelkit/reference/static"
)

// defaultSiteName — название, пока в site_settings нет строки (до seed или настройки в админке).
const defaultSiteName = "Keelkit reference"

// newMux — все маршруты сайта. HTML-маршруты оборачиваются в html: данные шапки в контексте запроса.
func newMux(db *sql.DB) *http.ServeMux {
	html := web.WithSite(siteLoader(db))
	pagesH := pages.NewHandler(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz(db))
	mux.Handle("GET /static/", static.Handler())

	mux.Handle("GET /{$}", html(http.HandlerFunc(pagesH.Home)))
	mux.Handle("GET /{slug}", html(http.HandlerFunc(pagesH.Show)))

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
