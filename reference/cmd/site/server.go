package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/keelkit/keelkit/modules/files"
	"github.com/keelkit/keelkit/modules/images"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/blocks"
	"github.com/keelkit/keelkit/reference/internal/content/categories"
	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/content/people"
	"github.com/keelkit/keelkit/reference/internal/content/posts"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/filelib"
	"github.com/keelkit/keelkit/reference/internal/media"
	"github.com/keelkit/keelkit/reference/internal/menu"
	"github.com/keelkit/keelkit/reference/internal/seo"
	"github.com/keelkit/keelkit/reference/internal/settings"
	"github.com/keelkit/keelkit/reference/internal/web"
	"github.com/keelkit/keelkit/reference/static"
)

// reservedSlugs — первые сегменты путей, занятые маршрутами newMux. Страница с таким slug была бы не видна,
// поэтому админка их не принимает. Новый маршрут верхнего уровня — новая запись здесь; если маршрут — поддерево
// (/static/), сам путь без слеша тоже нужно занять, иначе его заберёт GET /{slug} (проверяет e2e).
var reservedSlugs = []string{"admin", "static", "media", "files", "healthz", strings.TrimPrefix(posts.BlogPath, "/")}

// newMux — все маршруты сайта. HTML-маршруты оборачиваются в html: данные шапки в контексте запроса.
// Маршруты админки — ещё и в adminOnly: защита от межсайтовых запросов (http.CrossOriginProtection),
// а всё, кроме входа, — в adminH.RequireUser (сессия и CSRF-токен).
// Изображения (images.Service) отдаются под web.MediaPrefix, файлы (files.Service) — под web.FilesPrefix
// обработчиками модулей.
func newMux(db *sql.DB, imgs *images.Service, fls *files.Service, cfg config) *http.ServeMux {
	lists := blockLists(people.NewHandler(db, imgs))
	pagesH := pages.NewHandler(db, imgs, fls, lists)
	postsH := posts.NewHandler(db, imgs, fls)
	sections := menuSections(postsH)
	html := web.WithSite(siteLoader(db, imgs, cfg.BaseURL, sections))
	pagesAdmin := pages.NewAdmin(db, imgs, media.AdminPath, fls, filelib.AdminPath, reservedSlugs, pageUsage(db), lists)
	postsAdmin := posts.NewAdmin(db, imgs, media.AdminPath, fls, filelib.AdminPath)
	categoriesAdmin := categories.NewAdmin(db, posts.CategoryPath, posts.EditPath)
	peopleAdmin := people.NewAdmin(db, imgs, media.AdminPath)
	settingsAdmin := settings.NewAdmin(db, imgs, media.AdminPath)
	menuAdmin := menu.NewAdmin(db, siteMenus, sections)
	mediaAdmin := media.NewAdmin(imgs, imageUsage(db))
	filesAdmin := filelib.NewAdmin(fls, fileUsage(db))
	adminH := admin.NewHandler(db, admin.Options{
		SecureCookies: cfg.CookieSecure,
		Now:           cfg.now,
		Counters:      adminCounters(db),
		Sections: []admin.Section{
			{Title: "Страницы", URL: pages.AdminPath, Icon: "pages"},
			{Title: "Посты", URL: posts.AdminPath, Icon: "posts"},
			{Title: "Категории", URL: categories.AdminPath, Icon: "tags"},
			{Title: "Люди", URL: people.AdminPath, Icon: "people"},
			{Title: "Медиатека", URL: media.AdminPath, Icon: "image"},
			{Title: "Файлы", URL: filelib.AdminPath, Icon: "files"},
		},
		SiteSections: []admin.Section{
			{Title: "Настройки", URL: settings.AdminPath, Icon: "settings"},
			{Title: "Меню", URL: menu.AdminPath, Icon: "menu"},
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
	mux.Handle("GET "+web.FilesPrefix, http.StripPrefix(strings.TrimSuffix(web.FilesPrefix, "/"), fls.Handler()))
	mux.Handle("GET /files", html(http.HandlerFunc(web.NotFound)))

	var sitemap []seo.Source
	for _, s := range sitemapSources(pagesH, postsH) {
		sitemap = append(sitemap, s.URLs)
	}
	mux.Handle("GET "+seo.SitemapPath, seo.Sitemap(cfg.BaseURL, sitemap...))
	mux.Handle("GET "+seo.FeedPath, seo.Feed(cfg.BaseURL, feedSource(db, postsH)))
	mux.Handle("GET "+seo.RobotsPath, seo.Robots(cfg.BaseURL, "/admin/"))

	// Согласие с баннером cookies: только POST, GET /cookie-notice — обычный /{slug}.
	mux.Handle("POST "+web.CookieNoticePath, crossOrigin.Handler(web.AcceptCookies(cfg.CookieSecure)))
	mux.Handle("GET /{$}", html(http.HandlerFunc(pagesH.Home)))
	mux.Handle("GET /{slug}", html(http.HandlerFunc(pagesH.Show)))
	mux.Handle("GET "+posts.BlogPath, html(http.HandlerFunc(postsH.Blog)))
	mux.Handle("GET "+posts.BlogPath+"/{slug...}", html(http.HandlerFunc(postsH.Show)))
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
	mux.Handle("POST /admin/pages/live", signedIn(pagesAdmin.Live))
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
	mux.Handle("POST /admin/posts/live", signedIn(postsAdmin.Live))
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
	mux.Handle("GET "+people.AdminPath, signedIn(peopleAdmin.List))
	mux.Handle("GET "+people.AdminPath+"/new", signedIn(peopleAdmin.New))
	mux.Handle("POST "+people.AdminPath, signedIn(peopleAdmin.Create))
	mux.Handle("POST "+people.AdminPath+"/order", signedIn(peopleAdmin.Order))
	mux.Handle("POST "+people.AdminPath+"/photo", signedIn(peopleAdmin.Photo))
	mux.Handle("GET "+people.AdminPath+"/{id}", signedIn(peopleAdmin.Edit))
	mux.Handle("POST "+people.AdminPath+"/{id}", signedIn(peopleAdmin.Update))
	mux.Handle("GET "+people.AdminPath+"/{id}/delete", signedIn(peopleAdmin.ConfirmDelete))
	mux.Handle("POST "+people.AdminPath+"/{id}/delete", signedIn(peopleAdmin.Delete))
	mux.Handle("GET "+settings.AdminPath, signedIn(settingsAdmin.Edit))
	mux.Handle("POST "+settings.AdminPath, signedIn(settingsAdmin.Update))
	mux.Handle("POST "+settings.AdminPath+"/image", signedIn(settingsAdmin.Image))
	mux.Handle("GET "+menu.AdminPath, signedIn(menuAdmin.List))
	mux.Handle("GET "+menu.AdminPath+"/{name}", signedIn(menuAdmin.Edit))
	mux.Handle("POST "+menu.AdminPath+"/{name}", signedIn(menuAdmin.Update))
	mux.Handle("GET /admin/media", signedIn(mediaAdmin.List))
	// Лимит тела — снаружи signedIn: форма без JavaScript передаёт CSRF-токен в multipart, RequireUser читает его первым.
	mux.Handle("POST /admin/media", maxBody(media.MaxUploadRequest, signedIn(mediaAdmin.Upload)))
	mux.Handle("GET /admin/media/{id}", signedIn(mediaAdmin.Show))
	mux.Handle("GET /admin/media/{id}/delete", signedIn(mediaAdmin.ConfirmDelete))
	mux.Handle("POST /admin/media/{id}/delete", signedIn(mediaAdmin.Delete))
	mux.Handle("GET "+filelib.AdminPath, signedIn(filesAdmin.List))
	mux.Handle("POST "+filelib.AdminPath, maxBody(filesAdmin.MaxUploadRequest(), signedIn(filesAdmin.Upload)))
	mux.Handle("GET "+filelib.AdminPath+"/{id}", signedIn(filesAdmin.Show))
	mux.Handle("POST "+filelib.AdminPath+"/{id}/replace", maxBody(filesAdmin.MaxUploadRequest(), signedIn(filesAdmin.Replace)))
	mux.Handle("GET "+filelib.AdminPath+"/{id}/delete", signedIn(filesAdmin.ConfirmDelete))
	mux.Handle("POST "+filelib.AdminPath+"/{id}/delete", signedIn(filesAdmin.Delete))
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

// socialLinks — ссылки на соцсети из настроек для подвала.
func socialLinks(list []settings.Social) []web.Link {
	links := make([]web.Link, 0, len(list))
	for _, s := range list {
		links = append(links, web.Link{Title: s.Name, URL: s.URL})
	}
	return links
}

// siteLoader собирает данные для layout: настройки сайта (название, описание, картинка для соцсетей, контакты,
// баннер cookies, коды подтверждения), меню (internal/menu), адрес сайта и ленты.
func siteLoader(db *sql.DB, imgs *images.Service, baseURL string, sections []menu.Section) func(context.Context) (web.Site, error) {
	sq := settings.New(db)
	mq := menu.New(db)
	return func(ctx context.Context) (web.Site, error) {
		s, err := settings.Get(ctx, sq)
		if err != nil {
			return web.Site{}, err
		}
		site := web.Site{
			Name: s.Name, Description: s.Description, BaseURL: baseURL, Feed: seo.FeedPath,
			Contacts:     web.Contacts{Address: s.Address, Phone: s.Phone, Email: s.Email},
			Social:       socialLinks(s.Social),
			Cookie:       web.CookieNotice{Text: s.CookieText, Link: s.CookieLink},
			Verification: web.Verification{Yandex: s.Yandex, Google: s.Google},
		}
		if s.ShareImage != "" {
			img, err := imgs.Get(ctx, s.ShareImage)
			switch {
			case errors.Is(err, images.ErrNotFound):
			case err != nil:
				return web.Site{}, fmt.Errorf("share image: %w", err)
			default:
				site.ShareImage = img
			}
		}
		if site.Menus, err = menu.Site(ctx, mq, sections); err != nil {
			return web.Site{}, err
		}
		return site, nil
	}
}

// siteMenus — меню из site.yaml (menus) по порядку: имя, название и место в админке. Вывод — internal/web/layout.templ
// (main — шапка, footer — подвал). TestSpecMenusMatchCode сверяет имена со спецификацией.
var siteMenus = []menu.Menu{
	{Name: "main", Title: "Основное меню", Hint: "В шапке каждой страницы."},
	{Name: "footer", Title: "Меню в подвале", Hint: "В подвале каждой страницы."},
}

// menuSections — разделы сайта, на которые может вести пункт меню: лента блога, подпункты — категории с постами.
// Новый тип со своей лентой добавляет сюда раздел.
func menuSections(postsH *posts.Handler) []menu.Section {
	return []menu.Section{
		{Key: "blog", Title: "Блог", Path: posts.BlogPath, ChildrenTitle: "категории", Children: postsH.CategoryLinks},
	}
}

// blockLists — источники записей блоков-списков (blocks.ListKinds): вывод на сайте и раздел админки.
// Новый блок-список — запись здесь; TestBlockListsMatchKinds сверяет ключи с blocks.ListKinds.
func blockLists(peopleH *people.Handler) blocks.ListSources {
	return blocks.ListSources{
		"people": {Render: peopleH.Cards, AdminURL: people.AdminPath},
	}
}

// pageUsage — что ссылается на страницу (раздел страниц не даёт удалить её и показывает список): пункты меню.
func pageUsage(db *sql.DB) pages.UsageFunc {
	mq := menu.New(db)
	return func(ctx context.Context, pageID int64) ([]pages.Usage, error) {
		names, err := menu.MenusUsingPage(ctx, mq, pageID)
		if err != nil {
			return nil, fmt.Errorf("menus using page: %w", err)
		}
		used := make([]pages.Usage, 0, len(names))
		for _, name := range names {
			title := name
			for _, m := range siteMenus {
				if m.Name == name {
					title = m.Title
				}
			}
			used = append(used, pages.Usage{Title: title, URL: menu.EditPath(name)})
		}
		return used, nil
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
		s, err := settings.Get(ctx, sq)
		if err != nil {
			return seo.Channel{}, nil, err
		}
		items, err := postsH.FeedItems(ctx)
		if err != nil {
			return seo.Channel{}, nil, fmt.Errorf("feed items: %w", err)
		}
		return seo.Channel{Title: s.Name, Description: s.Description, Language: "ru"}, items, nil
	}
}

// imageUsage — где используется изображение (медиатека показывает список и не даёт удалить используемое).
// Новый тип контента со ссылками на изображения добавляет сюда свой запрос.
func imageUsage(db *sql.DB) media.UsageFunc {
	pq, sq, setq, peq := pages.New(db), posts.New(db), settings.New(db), people.New(db)
	return func(ctx context.Context, imageID string) ([]media.Usage, error) {
		var used []media.Usage
		inSettings, err := settings.UseImage(ctx, setq, imageID)
		if err != nil {
			return nil, fmt.Errorf("settings using image: %w", err)
		}
		if inSettings {
			used = append(used, media.Usage{Title: "Настройки: картинка для соцсетей", URL: settings.AdminPath})
		}
		rows, err := pq.PagesUsingImage(ctx, imageID)
		if err != nil {
			return nil, fmt.Errorf("pages using image: %w", err)
		}
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
		peopleRows, err := people.UsingImage(ctx, peq, imageID)
		if err != nil {
			return nil, fmt.Errorf("people using image: %w", err)
		}
		for _, p := range peopleRows {
			used = append(used, media.Usage{Title: "Люди: " + p.Name, URL: people.EditPath(p.ID)})
		}
		return used, nil
	}
}

// fileUsage — где используется файл (блоки file страниц и постов, в том числе черновики): раздел «Файлы» не даёт
// удалить такой файл. Новый тип контента со ссылками на файлы добавляет сюда свой запрос.
func fileUsage(db *sql.DB) filelib.UsageFunc {
	pq, sq := pages.New(db), posts.New(db)
	return func(ctx context.Context, fileID string) ([]filelib.Usage, error) {
		var used []filelib.Usage
		rows, err := pq.PagesUsingFile(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("pages using file: %w", err)
		}
		for _, p := range rows {
			used = append(used, filelib.Usage{Title: "Страница «" + p.Title + "»", URL: pages.EditPath(p.ID)})
		}
		postRows, err := sq.PostsUsingFile(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("posts using file: %w", err)
		}
		for _, p := range postRows {
			used = append(used, filelib.Usage{Title: "Пост «" + p.Title + "»", URL: posts.EditPath(p.ID)})
		}
		return used, nil
	}
}

// adminCounters — счётчики контента для дашборда админки.
func adminCounters(db *sql.DB) func(context.Context) ([]admin.Counter, error) {
	pq, sq, cq, peq := pages.New(db), posts.New(db), categories.New(db), people.New(db)
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
		npe, err := peq.CountPeople(ctx)
		if err != nil {
			return nil, fmt.Errorf("count people: %w", err)
		}
		return []admin.Counter{
			{Label: "Страниц", Value: n, URL: pages.AdminPath},
			{Label: "Постов", Value: np, URL: posts.AdminPath},
			{Label: "Категорий", Value: nc, URL: categories.AdminPath},
			{Label: "Людей", Value: npe, URL: people.AdminPath},
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
