// Package people — тип контента «люди» (site.yaml: content.people): записи без своего адреса.
//
// На сайте людей выводит блок-список people (internal/blocks) на любой странице: этот пакет отдаёт вывод
// (Handler.Cards), блоки о типе не знают — связывает их cmd/site (blockLists). Черновика нет: правки сразу на сайте.
// Порядок — ручной (feature order): кнопки ↑ ↓ в списке админки.
package people

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/keelkit/keelkit/modules/images"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// AdminPath — раздел людей в админке.
const AdminPath = "/admin/people"

// Ограничения формы.
const (
	maxNameLen   = 100      // символов
	maxRoleLen   = 200      // символов
	maxFormSize  = 64 << 10 // байт
	libraryLimit = 200      // изображений в выборе фото: последние загруженные
)

// Images — то, что пакету нужно от модуля images: фото и последние загруженные для выбора.
type Images interface {
	Get(ctx context.Context, id string) (*images.Image, error)
	List(ctx context.Context, limit, offset int) ([]*images.Image, error)
}

// Admin — раздел «Люди»: список с ручным порядком, создание, редактирование, удаление.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	db       *sql.DB
	q        *Queries
	media    Images
	mediaURL string // медиатека: ссылка «загрузите в медиатеку»
}

// NewAdmin создаёт раздел. mediaURL — адрес медиатеки в админке.
func NewAdmin(db *sql.DB, media Images, mediaURL string) *Admin {
	return &Admin{db: db, q: New(db), media: media, mediaURL: mediaURL}
}

// personForm — форма человека. ID == 0 — новый.
type personForm struct {
	ID       int64
	Name     string
	Role     string
	Photo    string // id изображения; пусто — без фото
	Errors   map[string]string
	Flash    string
	Image    *images.Image   // выбранное фото (превью и пункт в выборе); nil — не выбрано или его нет
	Library  []*images.Image // выбор фото: последние загруженные
	MediaURL string
}

// List — GET /admin/people.
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	list, err := a.q.ListPeople(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, ListPage(list, r.URL.Query().Has("deleted")))
}

// Order — POST /admin/people/order: кнопка ↑ или ↓ в списке (action — "up:<id>" или "down:<id>") сразу меняет
// порядок на сайте. С htmx — список заново (фрагмент), без него — 303 на список. Неизвестное действие — 400,
// нет такого человека — 404.
func (a *Admin) Order(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	op, arg, _ := strings.Cut(r.PostFormValue("action"), ":")
	id, err := strconv.ParseInt(arg, 10, 64)
	if (op != "up" && op != "down") || err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	err = move(r.Context(), a.db, id, op == "up")
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if !admin.IsHTMX(r) {
		http.Redirect(w, r, AdminPath, http.StatusSeeOther)
		return
	}
	list, err := a.q.ListPeople(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, Table(list))
}

// move поднимает (up) или опускает человека на одно место и переписывает sort_order всех по порядку (0, 1, 2…)
// одной транзакцией: так порядок остаётся плотным и без повторов, даже если строки вставляли в обход админки.
// Крайний в нужную сторону остаётся на месте. Нет такого — sql.ErrNoRows.
func move(ctx context.Context, db *sql.DB, id int64, up bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := New(tx)
	ids, err := q.ListPeopleIDs(ctx)
	if err != nil {
		return err
	}
	i := slices.Index(ids, id)
	if i < 0 {
		return sql.ErrNoRows
	}
	switch {
	case up && i > 0:
		ids[i-1], ids[i] = ids[i], ids[i-1]
	case !up && i < len(ids)-1:
		ids[i], ids[i+1] = ids[i+1], ids[i]
	}
	for n, pid := range ids {
		if err := q.SetPersonOrder(ctx, SetPersonOrderParams{SortOrder: int64(n), ID: pid}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// New — GET /admin/people/new.
func (a *Admin) New(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, personForm{})
}

// Edit — GET /admin/people/{id}.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Has("saved") {
		f.Flash = "Сохранено. Изменения уже на сайте."
	}
	a.render(w, r, http.StatusOK, f)
}

// Create — POST /admin/people.
func (a *Admin) Create(w http.ResponseWriter, r *http.Request) {
	a.submit(w, r, 0)
}

// Update — POST /admin/people/{id}.
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	a.submit(w, r, f.ID)
}

// Photo — POST /admin/people/photo (htmx): поле фото с превью выбранного.
func (a *Admin) Photo(w http.ResponseWriter, r *http.Request) {
	f, ok := a.parse(w, r, 0)
	if !ok {
		return
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, photoField(f))
}

func (a *Admin) submit(w http.ResponseWriter, r *http.Request, id int64) {
	f, ok := a.parse(w, r, id)
	if !ok {
		return
	}
	if err := a.env(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if validate(&f) {
		id, err := a.save(r.Context(), f)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			admin.NotFound(w, r)
		case err != nil:
			admin.ServerError(w, r, err)
		default:
			admin.Redirect(w, r, EditPath(id)+"?saved=1")
		}
		return
	}
	a.render(w, r, http.StatusUnprocessableEntity, f)
}

// render — форма: с htmx — только форма (ошибки), без него — страница. Медиатеку для выбора фото загружает,
// если её ещё не загрузил env.
func (a *Admin) render(w http.ResponseWriter, r *http.Request, status int, f personForm) {
	if f.MediaURL == "" {
		if err := a.env(r.Context(), &f); err != nil {
			admin.ServerError(w, r, err)
			return
		}
	}
	c := FormPage(f)
	if admin.IsHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, status, c)
}

// parse — поля из присланной формы (без проверки); тело больше лимита или не форма — 400.
func (a *Admin) parse(w http.ResponseWriter, r *http.Request, id int64) (personForm, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return personForm{}, false
	}
	return personForm{ID: id, Name: r.PostFormValue("name"), Role: r.PostFormValue("role"),
		Photo: strings.TrimSpace(r.PostFormValue("photo"))}, true
}

// env загружает выбранное фото и медиатеку для выбора.
func (a *Admin) env(ctx context.Context, f *personForm) error {
	f.MediaURL = a.mediaURL
	f.Image = nil
	if f.Photo != "" {
		img, err := a.media.Get(ctx, f.Photo)
		switch {
		case errors.Is(err, images.ErrNotFound):
		case err != nil:
			return fmt.Errorf("photo: %w", err)
		default:
			f.Image = img
		}
	}
	lib, err := a.media.List(ctx, libraryLimit, 0)
	if err != nil {
		return fmt.Errorf("media library: %w", err)
	}
	f.Library = lib
	return nil
}

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Фото должно быть уже загружено в f.Image.
func validate(f *personForm) bool {
	errs := map[string]string{}
	f.Name = strings.Join(strings.Fields(f.Name), " ")
	switch {
	case f.Name == "":
		errs["name"] = "Укажите имя."
	case utf8.RuneCountInString(f.Name) > maxNameLen:
		errs["name"] = fmt.Sprintf("Имя длиннее %d символов.", maxNameLen)
	}
	if f.Role = strings.Join(strings.Fields(f.Role), " "); utf8.RuneCountInString(f.Role) > maxRoleLen {
		errs["role"] = fmt.Sprintf("Должность длиннее %d символов.", maxRoleLen)
	}
	if f.Photo != "" && f.Image == nil {
		errs["photo"] = "Изображения нет в медиатеке: выберите другое."
	}
	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// save создаёт (f.ID == 0, в конец списка) или обновляет человека. Нет такого — sql.ErrNoRows.
func (a *Admin) save(ctx context.Context, f personForm) (int64, error) {
	photo := sql.NullString{String: f.Photo, Valid: f.Photo != ""}
	if f.ID == 0 {
		return a.q.CreatePerson(ctx, CreatePersonParams{Name: f.Name, Role: f.Role, Photo: photo})
	}
	n, err := a.q.UpdatePerson(ctx, UpdatePersonParams{Name: f.Name, Role: f.Role, Photo: photo, ID: f.ID})
	if err == nil && n == 0 {
		err = sql.ErrNoRows
	}
	return f.ID, err
}

// ConfirmDelete — GET /admin/people/{id}/delete: подтверждение.
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(f))
}

// Delete — POST /admin/people/{id}/delete. На людей ничего не ссылается: блок people выводит тех, кто есть.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if _, err := a.q.DeletePerson(r.Context(), f.ID); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	admin.Redirect(w, r, AdminPath+"?deleted=1")
}

// load загружает человека по {id} из пути; нет такого — 404 админки.
func (a *Admin) load(w http.ResponseWriter, r *http.Request) (personForm, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		admin.NotFound(w, r)
		return personForm{}, false
	}
	p, err := a.q.GetPerson(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return personForm{}, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return personForm{}, false
	}
	return personForm{ID: p.ID, Name: p.Name, Role: p.Role, Photo: p.Photo.String}, true
}

// EditPath — форма человека в админке.
func EditPath(id int64) string { return AdminPath + "/" + strconv.FormatInt(id, 10) }

func formAction(f personForm) string {
	if f.ID == 0 {
		return AdminPath
	}
	return EditPath(f.ID)
}

// photoOptions — изображения для выбора фото: медиатека и выбранное, если его там нет (загружено давно).
func photoOptions(f personForm) []*images.Image {
	if f.Image == nil || slices.ContainsFunc(f.Library, func(img *images.Image) bool { return img.ID == f.Image.ID }) {
		return f.Library
	}
	return append([]*images.Image{f.Image}, f.Library...)
}

func optionLabel(img *images.Image) string {
	name := img.Name
	if name == "" {
		name = img.ID
	}
	return fmt.Sprintf("%s · %d×%d", name, img.Width, img.Height)
}

// UsingImage — люди, у которых это фото (медиатека: «где используется»).
func UsingImage(ctx context.Context, q *Queries, imageID string) ([]PeopleUsingImageRow, error) {
	return q.PeopleUsingImage(ctx, sql.NullString{String: imageID, Valid: true})
}

// Seed — dev-данные: человек с таким именем, если его ещё нет (в конец списка). photo — id изображения или пусто.
func Seed(ctx context.Context, db DBTX, name, role, photo string) (int64, error) {
	q := New(db)
	id, err := q.GetPersonIDByName(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return q.CreatePerson(ctx, CreatePersonParams{Name: name, Role: role,
			Photo: sql.NullString{String: photo, Valid: photo != ""}})
	}
	return id, err
}
