// Package pages — тип контента «страницы» (site.yaml: content.pages): запросы sqlc, публичные хендлеры, шаблоны.
package pages

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/voffkin/keelkit/reference/internal/richtext"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// HomeSlug — slug страницы, которая показывается на главной (/).
const HomeSlug = "home"

// Handler — публичные страницы: / и /{slug}.
type Handler struct {
	q *Queries
}

func NewHandler(db DBTX) *Handler {
	return &Handler{q: New(db)}
}

// Home — GET /: опубликованная страница со slug home; пока её нет — заглушка с названием сайта.
// Черновиком главная не бывает (проверяет админка), но и черновик здесь не показался бы.
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

// Show — GET /{slug}: только опубликованные страницы, черновик — 404.
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

func (h *Handler) render(w http.ResponseWriter, r *http.Request, p Page) {
	body, err := richtext.Render(p.Body)
	if err != nil {
		web.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, View(p, body))
}
