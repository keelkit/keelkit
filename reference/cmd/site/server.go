package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/content/categories"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
	"github.com/voffkin/keelkit/reference/internal/content/posts"
	"github.com/voffkin/keelkit/reference/internal/database"
	"github.com/voffkin/keelkit/reference/internal/media"
	"github.com/voffkin/keelkit/reference/internal/seo"
	"github.com/voffkin/keelkit/reference/internal/settings"
	"github.com/voffkin/keelkit/reference/internal/web"
	"github.com/voffkin/keelkit/reference/static"
)

// defaultSiteName — название, пока в site_settings нет строки (до seed или настройки в админке).
const defaultSiteName = "Keelkit reference"

// reservedSlugs — первые сегменты путей, занятые маршрутами newMux. Страница с таким slug была бы не видна,
// поэтому админка их не принимает. Новый маршрут верхнего уровня — новая запись здесь; если маршрут — поддерево
// (/static/), сам путь без слеша тоже нужно занять, иначе его заберёт GET /{slug} (проверяет e2e).
var reservedSlugs = []string{"admin", "static", "media", "healthz", "blog"}

// newMux — все маршруты сайта. HTML-маршруты оборачиваются в html: данные шапки в контексте запроса.
// Маршруты админки — ещё и в adminOnly: защита от межсайтовых запросов (http.CrossOriginProtection),
// а всё, кроме входа, — в adminH.RequireUser (сессия и CSRF-токен).
// Изображения (images.Service) отдаются под web.MediaPrefix обработчиком модуля.
func newMux(db *sql.DB, imgs *images.Service, cfg config) *http.ServeMux {
	html := web.WithSite(siteLoader(db, cfg.BaseURL))
	pagesH := pages.NewHandler(db, imgs)
	pagesAdmin := pages.NewAdmin(db, imgs, media.AdminPath, reservedSlugs)
	postsH := posts.NewHandler(db, imgs)
	postsAdmin := posts.NewAdmin(db, imgs, media.AdminPath)
	categoriesAdmin := categories.NewAdmin(db, posts.CategoryPath, posts.EditPath)
	mediaAdmin := media.NewAdmin(imgs, imageUsage(db))
	adminH := admin.NewHandler(db, admin.Options{
		SecureCookies: cfg.CookieSecure,
		Counters:      adminCounters(db),
		Sections: []admin.Section{
			{Title: "Страницы", URL: pages.AdminPath, Icon: "pages"},
			{Title: "Посты", URL: posts.AdminPath, Icon: "posts"},
			{Title: "Категории", URL: categories.AdminPath, Icon: "tags"},
			{Title: "Медиатека", URL: media.AdminPath, Icon: "image"},
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

	var sitemap []seo.Source
	for _, s := range sitemapSources(pagesH, postsH) {
		sitemap = append(sitemap, s.URLs)
	}
	mux.Handle("GET "+seo.SitemapPath, seo.Sitemap(cfg.BaseURL, sitemap...))
	mux.Handle("GET "+seo.FeedPath, seo.Feed(cfg.BaseURL, feedSource(db, postsH)))
	mux.Handle("GET "+seo.RobotsPath, seo.Robots(cfg.BaseURL, "/admin/"))

	mux.Handle("GET /{$}", html(http.HandlerFunc(pagesH.Home)))
	mux.Handle("GET /{slug}", html(http.HandlerFunc(pagesH.Show)))
	mux.Handle("GET "+posts.BlogPath, html(http.HandlerFunc(postsH.Blog)))
	mux.Handle("GET "+posts.BlogPath+"/{slug}", html(http.HandlerFunc(postsH.Show)))
	mux.Handle("GET "+posts.BlogPath+"/category/{slug}", html(http.HandlerFunc(postsH.Category)))

	mux.Handle("GET /admin/login", adminOnly(adminH.LoginForm))
	mux.Handle("POST /admin/login", adminOnly(adminH.Login))
	mux.Handle("POST /admin/logout", signedIn(adminH.Logout))
	mux.Handle("GET /admin", signedIn(adminH.Dashboard))
	mux.Handle("GET /admin/{$}", http.RedirectHandler("/admin", http.StatusMovedPermanently))

	mux.Handle("GET /admin/pages", signedIn(pagesAdmin.List))
	mux.Handle("GET /admin/pages/new", signedIn(pagesAdmin.New))
	mux.Handle("POST /admin/pages", signedIn(pagesAdmin.Create))
	mux.Handle("POST /admin/pages/preview", signedIn(pagesAdmin.Preview))
	mux.Handle("POST /admin/pages/block", signedIn(pagesAdmin.Block))
	mux.Handle("POST /admin/pages/seo", signedIn(pagesAdmin.SEO))
	mux.Handle("GET /admin/pages/{id}", signedIn(pagesAdmin.Edit))
	mux.Handle("POST /admin/pages/{id}", signedIn(pagesAdmin.Update))
	mux.Handle("GET /admin/pages/{id}/view", signedIn(pagesAdmin.View))
	mux.Handle("POST /admin/pages/{id}/unpublish", signedIn(pagesAdmin.Unpublish))
	mux.Handle("GET /admin/pages/{id}/delete", signedIn(pagesAdmin.ConfirmDelete))
	mux.Handle("POST /admin/pages/{id}/delete", signedIn(pagesAdmin.Delete))
	mux.Handle("GET /admin/posts", signedIn(postsAdmin.List))
	mux.Handle("GET /admin/posts/new", signedIn(postsAdmin.New))
	mux.Handle("POST /admin/posts", signedIn(postsAdmin.Create))
	mux.Handle("POST /admin/posts/preview", signedIn(postsAdmin.Preview))
	mux.Handle("POST /admin/posts/block", signedIn(postsAdmin.Block))
	mux.Handle("POST /admin/posts/seo", signedIn(postsAdmin.SEO))
	mux.Handle("POST /admin/posts/cover", signedIn(postsAdmin.Cover))
	mux.Handle("GET /admin/posts/{id}", signedIn(postsAdmin.Edit))
	mux.Handle("POST /admin/posts/{id}", signedIn(postsAdmin.Update))
	mux.Handle("GET /admin/posts/{id}/view", signedIn(postsAdmin.View))
	mux.Handle("POST /admin/posts/{id}/unpublish", signedIn(postsAdmin.Unpublish))
	mux.Handle("GET /admin/posts/{id}/delete", signedIn(postsAdmin.ConfirmDelete))
	mux.Handle("POST /admin/posts/{id}/delete", signedIn(postsAdmin.Delete))
	mux.Handle("GET /admin/categories", signedIn(categoriesAdmin.List))
	mux.Handle("GET /admin/categories/new", signedIn(categoriesAdmin.New))
	mux.Handle("POST /admin/categories", signedIn(categoriesAdmin.Create))
	mux.Handle("GET /admin/categories/{id}", signedIn(categoriesAdmin.Edit))
	mux.Handle("POST /admin/categories/{id}", signedIn(categoriesAdmin.Update))
	mux.Handle("GET /admin/categories/{id}/delete", signedIn(categoriesAdmin.ConfirmDelete))
	mux.Handle("POST /admin/categories/{id}/delete", signedIn(categoriesAdmin.Delete))
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

// siteLoader собирает данные для layout: название и описание сайта, меню из страниц (кроме главной) и блог,
// адрес сайта и ленты.
func siteLoader(db *sql.DB, baseURL string) func(context.Context) (web.Site, error) {
	sq := settings.New(db)
	pq := pages.New(db)
	return func(ctx context.Context) (web.Site, error) {
		name, description, err := siteInfo(ctx, sq)
		if err != nil {
			return web.Site{}, err
		}
		site := web.Site{Name: name, Description: description, BaseURL: baseURL, Feed: seo.FeedPath}

		links, err := pq.ListPageLinks(ctx, pages.HomeSlug)
		if err != nil {
			return web.Site{}, fmt.Errorf("page links: %w", err)
		}
		for _, l := range links {
			site.Nav = append(site.Nav, web.Link{Title: l.Title, URL: pages.PublicPath(l.Slug)})
		}
		site.Nav = append(site.Nav, web.Link{Title: "Блог", URL: posts.BlogPath})
		return site, nil
	}
}

// siteInfo — название и описание сайта из site_settings; строки ещё нет — defaultSiteName.
func siteInfo(ctx context.Context, sq *settings.Queries) (name, description string, err error) {
	s, err := sq.GetSiteSettings(ctx)
	switch {
	case err == nil:
		return s.Name, s.Description, nil
	case errors.Is(err, sql.ErrNoRows):
		return defaultSiteName, "", nil
	default:
		return "", "", fmt.Errorf("site settings: %w", err)
	}
}

// sitemapSource — адреса для sitemap одного типа контента.
type sitemapSource struct {
	Type string // имя типа в site.yaml
	URLs seo.Source
}

// sitemapSources — типы контента с feature sitemap в site.yaml, по порядку (страницы, затем блог).
// Новый тип с этой feature добавляет сюда свой SitemapURLs; TestSpecFeaturesMatchCode сверяет список со спецификацией.
func sitemapSources(pagesH *pages.Handler, postsH *posts.Handler) []sitemapSource {
	return []sitemapSource{
		{Type: "pages", URLs: pagesH.SitemapURLs},
		{Type: "posts", URLs: postsH.SitemapURLs},
	}
}

// feedTypes — типы контента с feature rss в site.yaml: лента собирается из их записей (пока один тип — посты).
// TestSpecFeaturesMatchCode сверяет список со спецификацией.
var feedTypes = []string{"posts"}

// feedSource — RSS-лента: название и описание сайта, последние посты.
func feedSource(db *sql.DB, postsH *posts.Handler) seo.FeedSource {
	sq := settings.New(db)
	return func(ctx context.Context) (seo.Channel, []seo.Item, error) {
		name, description, err := siteInfo(ctx, sq)
		if err != nil {
			return seo.Channel{}, nil, err
		}
		items, err := postsH.FeedItems(ctx)
		if err != nil {
			return seo.Channel{}, nil, fmt.Errorf("feed items: %w", err)
		}
		return seo.Channel{Title: name, Description: description, Language: "ru"}, items, nil
	}
}

// imageUsage — где используется изображение (медиатека показывает список и не даёт удалить используемое).
// Новый тип контента со ссылками на изображения добавляет сюда свой запрос.
func imageUsage(db *sql.DB) media.UsageFunc {
	pq, sq := pages.New(db), posts.New(db)
	return func(ctx context.Context, imageID string) ([]media.Usage, error) {
		rows, err := pq.PagesUsingImage(ctx, imageID)
		if err != nil {
			return nil, fmt.Errorf("pages using image: %w", err)
		}
		used := make([]media.Usage, 0, len(rows))
		for _, p := range rows {
			used = append(used, media.Usage{Title: "Страница «" + p.Title + "»", URL: pages.EditPath(p.ID)})
		}
		postRows, err := sq.PostsUsingImage(ctx, imageID)
		if err != nil {
			return nil, fmt.Errorf("posts using image: %w", err)
		}
		for _, p := range postRows {
			used = append(used, media.Usage{Title: "Пост «" + p.Title + "»", URL: posts.EditPath(p.ID)})
		}
		return used, nil
	}
}

// adminCounters — счётчики контента для дашборда админки.
func adminCounters(db *sql.DB) func(context.Context) ([]admin.Counter, error) {
	pq, sq, cq := pages.New(db), posts.New(db), categories.New(db)
	return func(ctx context.Context) ([]admin.Counter, error) {
		n, err := pq.CountPages(ctx)
		if err != nil {
			return nil, fmt.Errorf("count pages: %w", err)
		}
		np, err := sq.CountPosts(ctx)
		if err != nil {
			return nil, fmt.Errorf("count posts: %w", err)
		}
		nc, err := cq.CountCategories(ctx)
		if err != nil {
			return nil, fmt.Errorf("count categories: %w", err)
		}
		return []admin.Counter{
			{Label: "Страниц", Value: n, URL: pages.AdminPath},
			{Label: "Постов", Value: np, URL: posts.AdminPath},
			{Label: "Категорий", Value: nc, URL: categories.AdminPath},
		}, nil
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
