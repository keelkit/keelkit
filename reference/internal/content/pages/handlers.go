// Package pages — тип контента «страницы» (site.yaml: content.pages): запросы sqlc, публичные хендлеры, шаблоны.
//
// Содержимое страницы — две копии в page_contents: черновик (есть всегда) и опубликованная (если страница на
// сайте). Сайт показывает только опубликованную; админка правит черновик и публикует его копированием.
package pages

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// HomeSlug — slug страницы, которая показывается на главной (/).
const HomeSlug = "home"

// descriptionLen — длина описания страницы (meta description) из текста блоков, символов.
const descriptionLen = 160

// Handler — публичные страницы: / и /{slug}.
type Handler struct {
	q      *Queries
	images blocks.Images
}

func NewHandler(db DBTX, imgs blocks.Images) *Handler {
	return &Handler{q: New(db), images: imgs}
}

// Home — GET /: опубликованная страница со slug home; пока её нет — заглушка с названием сайта.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	p, err := h.q.GetPublishedPageBySlug(r.Context(), HomeSlug)
	if errors.Is(err, sql.ErrNoRows) {
		web.Render(w, r, http.StatusOK, HomeEmpty())
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	h.render(w, r, p)
}

// Show — GET /{slug}: только опубликованные страницы; нет опубликованной копии с таким slug — 404.
// Главная доступна только по /: /home перенаправляется туда.
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == HomeSlug {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
		return
	}
	p, err := h.q.GetPublishedPageBySlug(r.Context(), slug)
	if errors.Is(err, sql.ErrNoRows) {
		web.NotFound(w, r)
		return
	}
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	h.render(w, r, p)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, p GetPublishedPageBySlugRow) {
	list, err := blocks.Unmarshal(p.Blocks)
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	res, err := blocks.Resolve(r.Context(), h.images, list)
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, View(viewData{ID: p.PageID, Title: p.Title, Slug: p.Slug, Blocks: list, Resolved: res}))
}
