package pages

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/richtext"
	"github.com/keelkit/keelkit/reference/internal/slug"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// AdminPath — раздел страниц в админке.
const AdminPath = "/admin/pages"

// Ограничения полей формы.
const (
	maxTitleLen = 200     // символов
	maxBodyLen  = 1 << 20 // байт Markdown
	maxFormSize = 2 << 20 // байт всего запроса
)

// Admin — раздел «Страницы» в админке: список, создание, редактирование, удаление, превью Markdown.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	q        *Queries
	reserved map[string]bool
}

// NewAdmin создаёт раздел. reserved — slug, занятые маршрутами сайта (admin, static, healthz…):
// такая страница была бы не видна, поэтому админка их не принимает.
func NewAdmin(db DBTX, reserved []string) *Admin {
	a := &Admin{q: New(db), reserved: make(map[string]bool, len(reserved))}
	for _, s := range reserved {
		a.reserved[s] = true
	}
	return a
}

// pageForm — данные формы страницы. ID == 0 — новая страница.
type pageForm struct {
	ID      int64
	Title   string
	Slug    string
	Body    string
	Errors  map[string]string // поле → сообщение
	Preview richtext.HTML
	Saved   bool // показать «Сохранено» после редиректа
}

// List — GET /admin/pages.
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	list, err := a.q.ListPages(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, ListPage(list, r.URL.Query().Has("deleted")))
}

// New — GET /admin/pages/new.
func (a *Admin) New(w http.ResponseWriter, r *http.Request) {
	a.renderForm(w, r, http.StatusOK, pageForm{})
}

// Edit — GET /admin/pages/{id}.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	p, ok := a.page(w, r)
	if !ok {
		return
	}
	f := pageForm{ID: p.ID, Title: p.Title, Slug: p.Slug, Body: p.Body, Saved: r.URL.Query().Has("saved")}
	a.renderForm(w, r, http.StatusOK, f)
}

// Create — POST /admin/pages.
func (a *Admin) Create(w http.ResponseWriter, r *http.Request) {
	f, ok := a.parseForm(w, r)
	if !ok {
		return
	}
	if a.validate(&f) {
		id, err := a.q.CreatePage(r.Context(), CreatePageParams{Title: f.Title, Slug: f.Slug, Body: f.Body})
		if a.saved(w, r, &f, id, err) {
			return
		}
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, f)
}

// Update — POST /admin/pages/{id}.
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return
	}
	f, ok := a.parseForm(w, r)
	if !ok {
		return
	}
	f.ID = id
	if a.validate(&f) {
		n, err := a.q.UpdatePage(r.Context(), UpdatePageParams{Title: f.Title, Slug: f.Slug, Body: f.Body, ID: id})
		if err == nil && n == 0 {
			admin.NotFound(w, r)
			return
		}
		if a.saved(w, r, &f, id, err) {
			return
		}
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, f)
}

// saved завершает сохранение: при успехе — редирект на форму с «Сохранено» (true), занятый slug — ошибка у поля (false),
// прочие ошибки — страница 500 (true).
func (a *Admin) saved(w http.ResponseWriter, r *http.Request, f *pageForm, id int64, err error) bool {
	if database.IsUniqueViolation(err) {
		f.Errors = map[string]string{"slug": fmt.Sprintf("Страница с адресом «%s» уже есть: выберите другой slug.", f.Slug)}
		return false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return true
	}
	redirect(w, r, editPath(id)+"?saved=1")
	return true
}

// ConfirmDelete — GET /admin/pages/{id}/delete: подтверждение удаления (без JavaScript).
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	p, ok := a.page(w, r)
	if !ok {
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(p))
}

// Delete — POST /admin/pages/{id}/delete.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return
	}
	n, err := a.q.DeletePage(r.Context(), id)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if n == 0 {
		admin.NotFound(w, r)
		return
	}
	redirect(w, r, AdminPath+"?deleted=1")
}

// Preview — POST /admin/pages/preview (htmx): фрагмент с отрисованным Markdown из поля body.
func (a *Admin) Preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	html, err := richtext.Render(normalizeNewlines(r.PostFormValue("body")))
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, Preview(html))
}

// page загружает страницу по {id} из пути; нет такой — 404 админки.
func (a *Admin) page(w http.ResponseWriter, r *http.Request) (Page, bool) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return Page{}, false
	}
	p, err := a.q.GetPage(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return Page{}, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return Page{}, false
	}
	return p, true
}

func (a *Admin) parseForm(w http.ResponseWriter, r *http.Request) (pageForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return pageForm{}, false
	}
	return pageForm{
		Title: r.PostFormValue("title"),
		Slug:  r.PostFormValue("slug"),
		Body:  r.PostFormValue("body"),
	}, true
}

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Уникальность slug проверяет БД при сохранении.
// Пустой slug составляется из заголовка; введённый вручную приводится к тому же виду (slug.Make).
func (a *Admin) validate(f *pageForm) bool {
	errs := map[string]string{}
	f.Title = strings.TrimSpace(f.Title)
	f.Body = normalizeNewlines(f.Body)

	switch {
	case f.Title == "":
		errs["title"] = "Укажите заголовок."
	case utf8.RuneCountInString(f.Title) > maxTitleLen:
		errs["title"] = fmt.Sprintf("Заголовок длиннее %d символов.", maxTitleLen)
	}

	if in := strings.TrimSpace(f.Slug); in != "" {
		f.Slug = slug.Make(in)
		if f.Slug == "" {
			errs["slug"] = "В slug нужны буквы или цифры."
			f.Slug = in
		}
	} else if f.Slug = slug.Make(f.Title); f.Slug == "" && f.Title != "" {
		errs["slug"] = "Из заголовка не получился адрес: укажите slug латиницей."
	}
	if a.reserved[f.Slug] {
		errs["slug"] = fmt.Sprintf("Адрес «/%s» занят разделом сайта: выберите другой slug.", f.Slug)
	}

	if len(f.Body) > maxBodyLen {
		errs["body"] = fmt.Sprintf("Текст больше %d КБ.", maxBodyLen>>10)
	}

	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// renderForm отдаёт форму: при htmx-запросе — только фрагмент формы (его подставит hx-swap), иначе — страницу целиком.
func (a *Admin) renderForm(w http.ResponseWriter, r *http.Request, status int, f pageForm) {
	preview, err := richtext.Render(f.Body)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Preview = preview
	c := FormPage(f)
	if isHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, status, c)
}

// redirect — после успешного POST: обычной форме 303, htmx — заголовок HX-Redirect (полный переход на адрес).
// target — всегда путь внутри раздела, собранный из констант и id.
func redirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // target не берётся из запроса
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// normalizeNewlines — браузер отправляет переводы строк textarea как CRLF; храним LF.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func editPath(id int64) string { return AdminPath + "/" + strconv.FormatInt(id, 10) }

// publicPath — адрес страницы на сайте: главная — /.
func publicPath(s string) string {
	if s == HomeSlug {
		return "/"
	}
	return "/" + s
}

// formatTime — время из БД (UTC RFC 3339) для списка; нераспознанное — как есть.
func formatTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("02.01.2006 15:04 UTC")
}
