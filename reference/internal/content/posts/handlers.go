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

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// BlogPath — лента блога на сайте.
const BlogPath = "/blog"

// Ограничения вывода.
const (
	perPage        = 10  // постов на странице ленты
	descriptionLen = 160 // символов описания поста (meta description) из текста блоков
	summaryLen     = 240 // символов анонса в ленте
)

// Handler — публичные страницы блога: лента, пост, лента категории.
type Handler struct {
	q      *Queries
	images blocks.Images
}

func NewHandler(db DBTX, imgs blocks.Images) *Handler {
	return &Handler{q: New(db), images: imgs}
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

// Blog — GET /blog: опубликованные посты, новые сверху, по perPage на странице (?page=N).
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

// Category — GET /blog/category/{slug}: опубликованные посты категории; нет категории — 404.
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
	return card{entry: e, Cover: cover, Summary: blocks.Summary(list, summaryLen)}, nil
}

// Show — GET /blog/{slug}: опубликованный пост, дата публикации которого наступила; иначе 404.
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	p, err := h.q.GetPublishedPostBySlug(r.Context(), GetPublishedPostBySlugParams{Slug: r.PathValue("slug"), Now: nowDB()})
	if errors.Is(err, sql.ErrNoRows) {
		web.NotFound(w, r)
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	v, err := newViewData(r.Context(), h.images, entry(p))
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, View(v))
}

// viewData — пост для публичного шаблона: опубликованная копия или (Preview) черновик в админке.
type viewData struct {
	entry
	List     []blocks.Block
	Resolved blocks.Resolved
	Cover    *images.Image
	Preview  bool // предпросмотр черновика: плашка со ссылкой в редактор
}

func newViewData(ctx context.Context, imgs blocks.Images, e entry) (viewData, error) {
	list, err := blocks.Unmarshal(e.Blocks)
	if err != nil {
		return viewData{}, err
	}
	res, err := blocks.Resolve(ctx, imgs, list)
	if err != nil {
		return viewData{}, err
	}
	cover, err := resolveCover(ctx, imgs, e.Cover.String)
	if err != nil {
		return viewData{}, err
	}
	return viewData{entry: e, List: list, Resolved: res, Cover: cover}, nil
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
