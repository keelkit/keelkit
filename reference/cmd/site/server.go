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

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
	"github.com/voffkin/keelkit/reference/internal/database"
	"github.com/voffkin/keelkit/reference/internal/media"
	"github.com/voffkin/keelkit/reference/internal/settings"
	"github.com/voffkin/keelkit/reference/internal/web"
	"github.com/voffkin/keelkit/reference/static"
)

// defaultSiteName — название, пока в site_settings нет строки (до seed или настройки в админке).
const defaultSiteName = "Keelkit reference"

// reservedSlugs — первые сегменты путей, занятые маршрутами newMux. Страница с таким slug была бы не видна,
// поэтому админка их не принимает. Новый маршрут верхнего уровня — новая запись здесь; если маршрут — поддерево
// (/static/), сам путь без слеша тоже нужно занять, иначе его заберёт GET /{slug} (проверяет e2e).
var reservedSlugs = []string{"admin", "static", "media", "healthz"}

// newMux — все маршруты сайта. HTML-маршруты оборачиваются в html: данные шапки в контексте запроса.
// Маршруты админки — ещё и в adminOnly: защита от межсайтовых запросов (http.CrossOriginProtection),
// а всё, кроме входа, — в adminH.RequireUser (сессия и CSRF-токен).
// Изображения (images.Service) отдаются под web.MediaPrefix обработчиком модуля.
func newMux(db *sql.DB, imgs *images.Service, cfg config) *http.ServeMux {
	html := web.WithSite(siteLoader(db))
	pagesH := pages.NewHandler(db)
	pagesAdmin := pages.NewAdmin(db, reservedSlugs)
	mediaAdmin := media.NewAdmin(imgs)
	adminH := admin.NewHandler(db, admin.Options{
		SecureCookies: cfg.CookieSecure,
		Counters:      adminCounters(db),
		Sections: []admin.Section{
			{Title: "Страницы", URL: pages.AdminPath},
			{Title: "Медиатека", URL: media.AdminPath},
		},
	})
	crossOrigin := http.NewCrossOriginProtection()
	adminOnly := func(h http.HandlerFunc) http.Handler { return crossOrigin.Handler(html(h)) }
	signedIn := func(h http.HandlerFunc) http.Handler { return adminOnly(adminH.RequireUser(h).ServeHTTP) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz(db))
	mux.Handle("GET /static/", static.Handler())
	mux.Handle("GET /static", html(http.HandlerFunc(web.NotFound)))
	mux.Handle("GET "+web.MediaPrefix, http.StripPrefix(web.MediaPrefix, imgs.Handler()))
	mux.Handle("GET /media", html(http.HandlerFunc(web.NotFound)))

	mux.Handle("GET /{$}", html(http.HandlerFunc(pagesH.Home)))
	mux.Handle("GET /{slug}", html(http.HandlerFunc(pagesH.Show)))

	mux.Handle("GET /admin/login", adminOnly(adminH.LoginForm))
	mux.Handle("POST /admin/login", adminOnly(adminH.Login))
	mux.Handle("POST /admin/logout", signedIn(adminH.Logout))
	mux.Handle("GET /admin", signedIn(adminH.Dashboard))
	mux.Handle("GET /admin/{$}", http.RedirectHandler("/admin", http.StatusMovedPermanently))

	mux.Handle("GET /admin/pages", signedIn(pagesAdmin.List))
	mux.Handle("GET /admin/pages/new", signedIn(pagesAdmin.New))
	mux.Handle("POST /admin/pages", signedIn(pagesAdmin.Create))
	mux.Handle("POST /admin/pages/preview", signedIn(pagesAdmin.Preview))
	mux.Handle("GET /admin/pages/{id}", signedIn(pagesAdmin.Edit))
	mux.Handle("POST /admin/pages/{id}", signedIn(pagesAdmin.Update))
	mux.Handle("GET /admin/pages/{id}/view", signedIn(pagesAdmin.View))
	mux.Handle("GET /admin/pages/{id}/delete", signedIn(pagesAdmin.ConfirmDelete))
	mux.Handle("POST /admin/pages/{id}/delete", signedIn(pagesAdmin.Delete))
	mux.Handle("GET /admin/media", signedIn(mediaAdmin.List))
	// Лимит тела — снаружи signedIn: форма без JavaScript передаёт CSRF-токен в multipart, RequireUser читает его первым.
	mux.Handle("POST /admin/media", maxBody(media.MaxUploadRequest, signedIn(mediaAdmin.Upload)))
	mux.Handle("GET /admin/media/{id}", signedIn(mediaAdmin.Show))
	mux.Handle("GET /admin/media/{id}/delete", signedIn(mediaAdmin.ConfirmDelete))
	mux.Handle("POST /admin/media/{id}/delete", signedIn(mediaAdmin.Delete))
	// Остальное под /admin/ — 404 админки, но только после входа: без сессии — редирект на вход.
	mux.Handle("/admin/", signedIn(adminH.NotFound))

	// Всё остальное — оформленная 404.
	mux.Handle("/", html(http.HandlerFunc(web.NotFound)))
	return mux
}

// maxBody ограничивает тело запроса n байтами: чтение сверх лимита возвращает *http.MaxBytesError.
func maxBody(n int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
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
		return []admin.Counter{{Label: "Страниц", Value: n, URL: pages.AdminPath}}, nil
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
