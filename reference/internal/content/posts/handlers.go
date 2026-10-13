// Package posts — тип контента «посты» (site.yaml: content.posts): лента блога, страницы постов и категорий,
// раздел админки.
//
// Как у страниц, содержимое поста — две копии в post_contents: черновик (есть всегда) и опубликованная (если пост
// на сайте). Сайт показывает только опубликованную, и только когда наступила её дата публикации (publish_date).
package posts

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/seo"
	"github.com/voffkin/keelkit/reference/internal/seofields"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// BlogPath — раздел постов на сайте: лента, посты (BlogPath/slug) и ленты категорий (BlogPath/category/slug).
// Один сегмент пути; другой адрес раздела (например /news) — правка только этой константы
// (reference/AGENTS.md, «Адрес раздела постов»).
const BlogPath = "/blog"

// Ограничения вывода.
const (
	perPage        = 10  // постов на странице ленты
	descriptionLen = 160 // символов описания поста (meta description) из текста блоков
	summaryLen     = 240 // символов анонса в ленте из текста блоков
	feedLen        = 20  // последних постов в RSS
	feedSummaryLen = 500 // символов анонса в RSS из текста блоков
)

// Handler — публичные страницы блога: лента, пост, лента категории.
type Handler struct {
	q      *Queries
	images blocks.Images
	files  blocks.FileSource
}

// NewHandler создаёт хендлеры; fls — файлы блоков file.
func NewHandler(db DBTX, imgs blocks.Images, fls blocks.FileSource) *Handler {
	return &Handler{q: New(db), images: imgs, files: fls}
}

// entry — опубликованный пост из любого публичного запроса (у них одинаковые колонки).
type entry = ListPublishedPostsRow

// card — пост в ленте: анонс и обложка.
type card struct {
	entry
	Cover   *images.Image
	Summary string
}

// listView — лента: весь блог или категория.
type listView struct {
	Title    string
	Category string // название категории; пусто — весь блог
	Path     string // адрес ленты без ?page
	Cards    []card
	Page     int
	HasNext  bool
}

// Blog — GET BlogPath: опубликованные посты, новые сверху, по perPage на странице (?page=N).
func (h *Handler) Blog(w http.ResponseWriter, r *http.Request) {
	page, ok := pageNumber(r)
	if !ok {
		web.NotFound(w, r)
		return
	}
	ctx, now := r.Context(), nowDB()
	rows, err := h.q.ListPublishedPosts(ctx, ListPublishedPostsParams{Now: now, Lim: perPage + 1, Off: int64((page - 1) * perPage)})
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	h.renderList(w, r, listView{Title: "Блог", Path: BlogPath, Page: page}, rows)
}

// Category — GET BlogPath/category/{slug}: опубликованные посты категории; нет категории — 404.
func (h *Handler) Category(w http.ResponseWriter, r *http.Request) {
	page, ok := pageNumber(r)
	if !ok {
		web.NotFound(w, r)
		return
	}
	ctx := r.Context()
	c, err := h.q.GetCategoryBySlug(ctx, r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) {
		web.NotFound(w, r)
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	in, err := h.q.ListPublishedPostsInCategory(ctx, ListPublishedPostsInCategoryParams{
		Now: nowDB(), CategoryID: sql.NullInt64{Int64: c.ID, Valid: true}, Lim: perPage + 1, Off: int64((page - 1) * perPage),
	})
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	rows := make([]entry, len(in))
	for i, row := range in {
		rows[i] = entry(row)
	}
	h.renderList(w, r, listView{Title: c.Name, Category: c.Name, Path: CategoryPath(c.Slug), Page: page}, rows)
}

// renderList — лента из rows (perPage+1 строк: лишняя означает, что есть следующая страница).
// Пустая страница, кроме первой, — 404.
func (h *Handler) renderList(w http.ResponseWriter, r *http.Request, v listView, rows []entry) {
	if len(rows) == 0 && v.Page > 1 {
		web.NotFound(w, r)
		return
	}
	if len(rows) > perPage {
		rows, v.HasNext = rows[:perPage], true
	}
	for _, row := range rows {
		c, err := h.card(r.Context(), row)
		if err != nil {
			web.ServerError(w, r, err)
			return
		}
		v.Cards = append(v.Cards, c)
	}
	web.Render(w, r, http.StatusOK, ListPage(v))
}

func (h *Handler) card(ctx context.Context, e entry) (card, error) {
	list, err := blocks.Unmarshal(e.Blocks)
	if err != nil {
		return card{}, err
	}
	cover, err := resolveCover(ctx, h.images, e.Cover.String)
	if err != nil {
		return card{}, err
	}
	return card{entry: e, Cover: cover, Summary: summary(e, list, summaryLen)}, nil
}

// Show — GET BlogPath/{slug...}: опубликованный пост, дата публикации которого наступила. Иначе — 301 на текущий
// адрес поста, если путь — его старый адрес (post_redirects: прежний slug или адрес, вписанный в форме поста; путь
// любой, в том числе не по правилам slug и с «/») и пост на сайте, или 404.
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, err := h.q.GetPublishedPostBySlug(r.Context(), GetPublishedPostBySlugParams{Slug: slug, Now: nowDB()})
	if errors.Is(err, sql.ErrNoRows) {
		h.redirectOld(w, r, strings.Trim(slug, "/"))
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	v, err := newViewData(r.Context(), h.images, h.files, entry(p))
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, View(v))
}

// redirectOld — 301 со старого адреса (путь после BlogPath/ без «/» по краям) на текущий адрес поста, видимого на сайте; такого нет — 404.
func (h *Handler) redirectOld(w http.ResponseWriter, r *http.Request, slug string) {
	target, err := h.q.RedirectTarget(r.Context(), RedirectTargetParams{OldSlug: slug, Now: nowDB()})
	if errors.Is(err, sql.ErrNoRows) {
		web.NotFound(w, r)
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, PublicPath(target), http.StatusMovedPermanently)
}

// CategoryLinks — категории, в которых сейчас есть видимые посты, ссылками на их ленты (по названию): подпункты
// раздела блога в меню сайта (internal/menu). Пустая категория в меню не попадает — её лента была бы пустой.
func (h *Handler) CategoryLinks(ctx context.Context) ([]web.Link, error) {
	rows, err := h.q.ListCategoriesWithPosts(ctx, nowDB())
	if err != nil {
		return nil, err
	}
	links := make([]web.Link, 0, len(rows))
	for _, c := range rows {
		links = append(links, web.Link{Title: c.Name, URL: CategoryPath(c.Slug)})
	}
	return links, nil
}

// SitemapURLs — лента блога и посты, видимые на сайте, для sitemap (site.yaml: feature sitemap).
// lastmod поста — дата публикации, ленты — дата самого нового поста.
func (h *Handler) SitemapURLs(ctx context.Context) ([]seo.URL, error) {
	rows, err := h.q.ListPublishedPostDates(ctx, nowDB())
	if err != nil {
		return nil, err
	}
	urls := make([]seo.URL, 0, len(rows)+1)
	urls = append(urls, seo.URL{Path: BlogPath})
	for _, row := range rows {
		t, _ := parseDB(row.PublishDate)
		urls = append(urls, seo.URL{Path: PublicPath(row.Slug), LastMod: t})
	}
	if len(rows) > 0 {
		urls[0].LastMod = urls[1].LastMod
	}
	return urls, nil
}

// FeedItems — последние feedLen постов, видимых на сайте, для RSS (site.yaml: feature rss): анонс (summary),
// обложка — вложением. guid — по id поста: не меняется вместе со slug.
func (h *Handler) FeedItems(ctx context.Context) ([]seo.Item, error) {
	rows, err := h.q.ListPublishedPosts(ctx, ListPublishedPostsParams{Now: nowDB(), Lim: feedLen, Off: 0})
	if err != nil {
		return nil, err
	}
	items := make([]seo.Item, 0, len(rows))
	for _, row := range rows {
		list, err := blocks.Unmarshal(row.Blocks)
		if err != nil {
			return nil, err
		}
		published, _ := parseDB(row.PublishDate)
		it := seo.Item{
			Title:       row.Title,
			Path:        PublicPath(row.Slug),
			GUID:        "post-" + strconv.FormatInt(row.PostID, 10),
			Description: summary(row, list, feedSummaryLen),
			Category:    row.CategoryName,
			Published:   published,
		}
		cover, err := resolveCover(ctx, h.images, row.Cover.String)
		if err != nil {
			return nil, err
		}
		if v, ok := web.ShareImage(cover); ok {
			it.Enclosure = &seo.Enclosure{Path: web.MediaURL(v), Length: v.Bytes, Type: "image/jpeg"}
		}
		items = append(items, it)
	}
	return items, nil
}

// viewData — пост для публичного шаблона: опубликованная копия или (Preview) черновик в админке.
type viewData struct {
	entry
	List     []blocks.Block
	Resolved blocks.Resolved
	Files    blocks.Files // файлы блоков file
	Cover    *images.Image
	SEOImage *images.Image // картинка SEO (seo_image); nil — подстановка
	Preview  bool          // предпросмотр черновика: плашка со ссылкой в редактор
	Live     bool          // живой предпросмотр в редакторе (Admin.Live): без плашки, редактор рядом
}

// seo — SEO поста, заданное вручную.
func (v viewData) seo() seofields.Fields {
	return seofields.FromDB(v.SeoTitle, v.SeoDescription, v.SeoImage)
}

func newViewData(ctx context.Context, imgs blocks.Images, fls blocks.FileSource, e entry) (viewData, error) {
	list, err := blocks.Unmarshal(e.Blocks)
	if err != nil {
		return viewData{}, err
	}
	res, err := blocks.Resolve(ctx, imgs, list)
	if err != nil {
		return viewData{}, err
	}
	fs, err := blocks.ResolveFiles(ctx, fls, list)
	if err != nil {
		return viewData{}, err
	}
	cover, err := resolveCover(ctx, imgs, e.Cover.String)
	if err != nil {
		return viewData{}, err
	}
	v := viewData{entry: e, List: list, Resolved: res, Files: fs, Cover: cover}
	if v.SEOImage, err = v.seo().Resolve(ctx, imgs); err != nil {
		return viewData{}, err
	}
	return v, nil
}

// summary — анонс поста для ленты, RSS и описания страницы: ручной (excerpt) целиком, в одну строку; без него —
// начало текста блоков, не длиннее n символов.
func summary(e entry, list []blocks.Block, n int) string {
	if e.Excerpt != "" {
		return strings.Join(strings.Fields(e.Excerpt), " ")
	}
	return blocks.Summary(list, n)
}

// resolveCover — обложка по id; пустой id или изображения нет в медиатеке — nil.
func resolveCover(ctx context.Context, imgs blocks.Images, id string) (*images.Image, error) {
	if id == "" {
		return nil, nil
	}
	img, err := imgs.Get(ctx, id)
	if errors.Is(err, images.ErrNotFound) {
		return nil, nil
	}
	return img, err
}

// pageNumber — номер страницы ленты из ?page=; нет параметра — первая, неверный — ok == false (404).
func pageNumber(r *http.Request) (int, bool) {
	if !r.URL.Query().Has("page") {
		return 1, true
	}
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	return n, err == nil && n >= 1
}

// pagePath — адрес страницы n ленты path: первая — без ?page.
func pagePath(path string, n int) string {
	if n <= 1 {
		return path
	}
	return path + "?page=" + strconv.Itoa(n)
}

// PublicPath — адрес поста на сайте.
func PublicPath(slug string) string { return BlogPath + "/" + url.PathEscape(slug) }

// CategoryPath — лента категории на сайте.
func CategoryPath(slug string) string { return BlogPath + "/category/" + url.PathEscape(slug) }
