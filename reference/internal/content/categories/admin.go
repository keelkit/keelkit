// Package categories — тип контента «категории» (site.yaml: content.categories): справочник для постов блога.
//
// У категории нет черновика: правки видны на сайте сразу. Публичная страница категории (posts.CategoryPath)
// — в пакете posts: это лента постов. Здесь — только раздел админки и seed.
package categories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/database"
	"github.com/voffkin/keelkit/reference/internal/slug"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// AdminPath — раздел категорий в админке.
const AdminPath = "/admin/categories"

// Ограничения формы.
const (
	maxNameLen  = 100      // символов
	maxFormSize = 64 << 10 // байт
)

// Admin — раздел «Категории»: список, создание, редактирование, удаление неиспользуемых.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	q *Queries
	// publicPath — адрес категории на сайте (ссылка «Открыть на сайте»): страницу отдаёт пакет posts.
	publicPath func(slug string) string
	// postPath — форма поста в админке (список постов, из-за которых категорию нельзя удалить).
	postPath func(id int64) string
}

// NewAdmin создаёт раздел. publicPath и postPath — адреса из пакета posts: категории о нём не знают.
func NewAdmin(db DBTX, publicPath func(string) string, postPath func(int64) string) *Admin {
	return &Admin{q: New(db), publicPath: publicPath, postPath: postPath}
}

// categoryForm — форма категории. ID == 0 — новая.
type categoryForm struct {
	ID     int64
	Name   string
	Slug   string
	Errors map[string]string
	Flash  string
}

// usage — пост, который ссылается на категорию.
type usage struct {
	Title string
	URL   string
}

// List — GET /admin/categories.
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	list, err := a.q.ListCategories(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, ListPage(list, r.URL.Query().Has("deleted")))
}

// New — GET /admin/categories/new.
func (a *Admin) New(w http.ResponseWriter, r *http.Request) {
	web.Render(w, r, http.StatusOK, FormPage(a, categoryForm{}))
}

// Edit — GET /admin/categories/{id}.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Has("saved") {
		f.Flash = "Сохранено."
	}
	web.Render(w, r, http.StatusOK, FormPage(a, f))
}

// Create — POST /admin/categories.
func (a *Admin) Create(w http.ResponseWriter, r *http.Request) {
	a.submit(w, r, categoryForm{})
}

// Update — POST /admin/categories/{id}.
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	a.submit(w, r, categoryForm{ID: f.ID})
}

func (a *Admin) submit(w http.ResponseWriter, r *http.Request, f categoryForm) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	f.Name, f.Slug = r.PostFormValue("name"), r.PostFormValue("slug")
	if validate(&f) {
		id, err := a.save(r.Context(), f)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			admin.NotFound(w, r)
			return
		case database.IsUniqueViolation(err):
			f.Errors = map[string]string{"slug": fmt.Sprintf("Категория с адресом «%s» уже есть: выберите другой slug.", f.Slug)}
		case err != nil:
			admin.ServerError(w, r, err)
			return
		default:
			admin.Redirect(w, r, EditPath(id)+"?saved=1")
			return
		}
	}
	c := FormPage(a, f)
	if admin.IsHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, http.StatusUnprocessableEntity, c)
}

// save создаёт (f.ID == 0) или обновляет категорию. Категории нет — sql.ErrNoRows.
func (a *Admin) save(ctx context.Context, f categoryForm) (int64, error) {
	if f.ID == 0 {
		return a.q.CreateCategory(ctx, CreateCategoryParams{Name: f.Name, Slug: f.Slug})
	}
	n, err := a.q.UpdateCategory(ctx, UpdateCategoryParams{Name: f.Name, Slug: f.Slug, ID: f.ID})
	if err == nil && n == 0 {
		err = sql.ErrNoRows
	}
	return f.ID, err
}

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Уникальность slug проверяет БД.
// Пустой slug составляется из названия, введённый приводится к тому же виду (slug.Make).
func validate(f *categoryForm) bool {
	errs := map[string]string{}
	f.Name = strings.TrimSpace(f.Name)
	switch {
	case f.Name == "":
		errs["name"] = "Укажите название."
	case utf8.RuneCountInString(f.Name) > maxNameLen:
		errs["name"] = fmt.Sprintf("Название длиннее %d символов.", maxNameLen)
	}
	if in := strings.TrimSpace(f.Slug); in != "" {
		f.Slug = slug.Make(in)
		if f.Slug == "" {
			errs["slug"] = "В slug нужны буквы или цифры."
			f.Slug = in
		}
	} else if f.Slug = slug.Make(f.Name); f.Slug == "" && f.Name != "" {
		errs["slug"] = "Из названия не получился адрес: укажите slug латиницей."
	}
	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// ConfirmDelete — GET /admin/categories/{id}/delete: подтверждение; категорию с постами удалить нельзя (409 со списком).
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	used, err := a.usage(r.Context(), f.ID)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if len(used) > 0 {
		web.Render(w, r, http.StatusConflict, DeleteBlockedPage(f, used))
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(f))
}

// Delete — POST /admin/categories/{id}/delete. Посты ссылаются на категорию внешним ключом без ON DELETE:
// если категорию успели выбрать в посте, БД не даст её удалить — 409 со списком.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	_, err := a.q.DeleteCategory(r.Context(), f.ID)
	switch {
	case err == nil:
		admin.Redirect(w, r, AdminPath+"?deleted=1")
	case database.IsForeignKeyViolation(err):
		used, err := a.usage(r.Context(), f.ID)
		if err != nil {
			admin.ServerError(w, r, err)
			return
		}
		web.Render(w, r, http.StatusConflict, DeleteBlockedPage(f, used))
	default:
		admin.ServerError(w, r, err)
	}
}

func (a *Admin) usage(ctx context.Context, id int64) ([]usage, error) {
	rows, err := a.q.PostsInCategory(ctx, sql.NullInt64{Int64: id, Valid: true})
	if err != nil {
		return nil, err
	}
	used := make([]usage, 0, len(rows))
	for _, p := range rows {
		used = append(used, usage{Title: p.Title, URL: a.postPath(p.ID)})
	}
	return used, nil
}

// load загружает категорию по {id} из пути; нет такой — 404 админки.
func (a *Admin) load(w http.ResponseWriter, r *http.Request) (categoryForm, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		admin.NotFound(w, r)
		return categoryForm{}, false
	}
	c, err := a.q.GetCategory(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return categoryForm{}, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return categoryForm{}, false
	}
	return categoryForm{ID: c.ID, Name: c.Name, Slug: c.Slug}, true
}

// EditPath — форма категории в админке.
func EditPath(id int64) string { return AdminPath + "/" + strconv.FormatInt(id, 10) }

func formAction(f categoryForm) string {
	if f.ID == 0 {
		return AdminPath
	}
	return EditPath(f.ID)
}

// formatTime — время из БД (UTC RFC 3339) для списка; нераспознанное — как есть.
func formatTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("02.01.2006 15:04 UTC")
}

// Seed — dev-данные: категория со slug, если её ещё нет. Возвращает id (новой или существующей).
func Seed(ctx context.Context, db DBTX, name, slug string) (int64, error) {
	q := New(db)
	id, err := q.GetCategoryIDBySlug(ctx, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return q.CreateCategory(ctx, CreateCategoryParams{Name: name, Slug: slug})
	}
	return id, err
}
