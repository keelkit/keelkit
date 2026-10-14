// Package filelib — раздел «Файлы» в админке: загрузка документов, видео и SVG в модуль files, список, карточка
// файла с заменой без смены адреса и удаление. Файлы и метаданные хранит модуль; сайт обращается к ним только
// через files.Service. Изображения для вёрстки — медиатека (internal/media).
package filelib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/keelkit/keelkit/modules/files"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// AdminPath — раздел «Файлы» в админке.
const AdminPath = "/admin/files"

const (
	perPage         = 50      // файлов на странице раздела
	multipartMemory = 8 << 20 // часть multipart, которая держится в памяти; остальное — во временном файле
	fileField       = "file"
)

// Admin — раздел «Файлы». Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены;
// загрузку и замену — ещё и в лимит тела MaxUploadRequest (снаружи RequireUser, как у медиатеки).
type Admin struct {
	svc   *files.Service
	usage UsageFunc
}

// Usage — где используется файл: запись контента и ссылка на неё в админке.
type Usage struct {
	Title string
	URL   string
}

// UsageFunc — контент сайта, который ссылается на файл. Пакет не знает о типах контента: функцию собирает
// cmd/site (fileUsage).
type UsageFunc func(ctx context.Context, fileID string) ([]Usage, error)

// NewAdmin создаёт раздел над сервисом файлов сайта; usage — где используется файл.
func NewAdmin(svc *files.Service, usage UsageFunc) *Admin { return &Admin{svc: svc, usage: usage} }

// MaxUploadRequest — предел тела запроса загрузки и замены: лимит модуля (files.Config.MaxBytes) с запасом
// на поля формы, чтобы файл чуть больше лимита дочитался и пользователь получил понятную ошибку.
func (a *Admin) MaxUploadRequest() int64 { return a.svc.MaxBytes() + 1<<20 }

// uploadForm — состояние формы загрузки или замены: сообщение об ошибке или пусто.
type uploadForm struct {
	Error string
}

// listView — страница раздела.
type listView struct {
	Files    []*files.File
	Page     int
	HasNext  bool
	Form     uploadForm
	MaxBytes int64
	Uploaded bool // показать «Загружено» после редиректа
	Deleted  bool // показать «Удалено» после редиректа
}

// showView — карточка файла.
type showView struct {
	File     *files.File
	Used     []Usage
	Form     uploadForm // замена
	MaxBytes int64
	Replaced bool // показать «Заменено» после редиректа
}

// List — GET /admin/files: форма загрузки и файлы от новых к старым, по perPage на странице (?page=N).
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a.renderList(w, r, http.StatusOK, pageNumber(q.Get("page")), uploadForm{}, q.Has("uploaded"), q.Has("deleted"))
}

func (a *Admin) renderList(w http.ResponseWriter, r *http.Request, status, page int, f uploadForm, uploaded, deleted bool) {
	list, err := a.svc.List(r.Context(), perPage+1, (page-1)*perPage)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	v := listView{Files: list, Page: page, Form: f, MaxBytes: a.svc.MaxBytes(), Uploaded: uploaded, Deleted: deleted}
	if len(list) > perPage {
		v.Files, v.HasNext = list[:perPage], true
	}
	web.Render(w, r, status, ListPage(v))
}

// Upload — POST /admin/files: загрузка одного файла. Успех — редирект на раздел с «Загружено»; ошибка проверки
// файла — 422: при htmx только форма загрузки, без него — страница раздела с ошибкой.
func (a *Admin) Upload(w http.ResponseWriter, r *http.Request) {
	msg, err := a.receive(w, r, func(ctx context.Context, f io.Reader, name string) error {
		_, err := a.svc.Upload(ctx, f, name)
		return err
	})
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if msg == "" {
		admin.Redirect(w, r, AdminPath+"?uploaded=1")
		return
	}
	f := uploadForm{Error: msg}
	if admin.IsHTMX(r) {
		web.Render(w, r, http.StatusUnprocessableEntity, UploadForm(f, a.svc.MaxBytes()))
		return
	}
	a.renderList(w, r, http.StatusUnprocessableEntity, 1, f, false, false)
}

// Replace — POST /admin/files/{id}/replace: новое содержимое того же формата по прежнему адресу. Успех — редирект
// на карточку с «Заменено»; ошибка — 422: при htmx только форма замены, без него — карточка с ошибкой.
func (a *Admin) Replace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	msg, err := a.receive(w, r, func(ctx context.Context, f io.Reader, name string) error {
		_, err := a.svc.Replace(ctx, id, f, name)
		return err
	})
	switch {
	case errors.Is(err, files.ErrNotFound):
		admin.NotFound(w, r)
		return
	case err != nil:
		admin.ServerError(w, r, err)
		return
	case msg == "":
		admin.Redirect(w, r, ShowPath(id)+"?replaced=1")
		return
	}
	file, ok := a.file(w, r)
	if !ok {
		return
	}
	f := uploadForm{Error: msg}
	if admin.IsHTMX(r) {
		web.Render(w, r, http.StatusUnprocessableEntity, ReplaceForm(file, f, a.svc.MaxBytes()))
		return
	}
	a.renderShow(w, r, http.StatusUnprocessableEntity, file, f, false)
}

// receive разбирает форму с файлом и передаёт его store. Ошибка проверки файла (в том числе из store) —
// сообщение для пользователя; прочие ошибки store — err.
func (a *Admin) receive(w http.ResponseWriter, r *http.Request, store func(context.Context, io.Reader, string) error) (string, error) {
	// Тот же лимит, что снаружи: обработчик не полагается на то, как его смонтировали.
	r.Body = http.MaxBytesReader(w, r.Body, a.MaxUploadRequest())
	// G120 в gosec v2.29 срабатывает на любой ParseMultipartForm и не учитывает MaxBytesReader выше.
	if err := r.ParseMultipartForm(multipartMemory); err != nil { //nolint:gosec // тело ограничено MaxBytesReader
		if mbe := new(http.MaxBytesError); errors.As(err, &mbe) {
			return a.uploadError(files.ErrTooLarge), nil
		}
		return "Не удалось прочитать файл: отправьте форму ещё раз.", nil
	}
	file, hdr, err := r.FormFile(fileField)
	if errors.Is(err, http.ErrMissingFile) {
		return "Выберите файл.", nil
	}
	if err != nil {
		return "Не удалось прочитать файл: отправьте форму ещё раз.", nil
	}
	defer func() { _ = file.Close() }()
	if hdr.Size > a.svc.MaxBytes() {
		return a.uploadError(files.ErrTooLarge), nil
	}
	if err := store(r.Context(), file, hdr.Filename); err != nil {
		if msg := a.uploadError(err); msg != "" {
			return msg, nil
		}
		return "", err
	}
	return "", nil
}

// uploadError — понятное сообщение для ошибки проверки файла модулем; пусто — ошибка не проверки.
func (a *Admin) uploadError(err error) string {
	switch {
	case errors.Is(err, files.ErrTooLarge):
		return fmt.Sprintf("Файл больше %s: уменьшите его и загрузите снова.", web.FormatBytes(a.svc.MaxBytes()))
	case errors.Is(err, files.ErrUnsupportedFormat):
		return "Формат не поддерживается: загрузите PDF, видео MP4 или WebM, картинку SVG. Фотографии — в медиатеку."
	case errors.Is(err, files.ErrInvalidFile):
		return "Файл пустой или повреждён."
	case errors.Is(err, files.ErrFormatMismatch):
		return "Замена — только файлом того же формата: адрес файла сохраняет расширение. Другой формат загрузите новым файлом."
	}
	return ""
}

// Show — GET /admin/files/{id}: файл, его адрес, форма замены и где он используется.
func (a *Admin) Show(w http.ResponseWriter, r *http.Request) {
	f, ok := a.file(w, r)
	if !ok {
		return
	}
	a.renderShow(w, r, http.StatusOK, f, uploadForm{}, r.URL.Query().Has("replaced"))
}

func (a *Admin) renderShow(w http.ResponseWriter, r *http.Request, status int, f *files.File, form uploadForm, replaced bool) {
	used, err := a.usage(r.Context(), f.ID)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, status, ShowPage(showView{File: f, Used: used, Form: form, MaxBytes: a.svc.MaxBytes(), Replaced: replaced}))
}

// ConfirmDelete — GET /admin/files/{id}/delete: подтверждение удаления (без JavaScript).
// Используемый файл удалить нельзя — сразу 409 со списком мест, где он стоит.
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.file(w, r)
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

// Delete — POST /admin/files/{id}/delete. Файл, на который ссылается контент сайта (внешний ключ на files_files
// без ON DELETE), модуль не удаляет — 409 с объяснением.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.file(w, r)
	if !ok {
		return
	}
	err := a.svc.Delete(r.Context(), f.ID)
	switch {
	case err == nil:
		admin.Redirect(w, r, AdminPath+"?deleted=1")
	case errors.Is(err, files.ErrNotFound):
		admin.NotFound(w, r)
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

// file загружает файл по {id} из пути; нет такого — 404 админки.
func (a *Admin) file(w http.ResponseWriter, r *http.Request) (*files.File, bool) {
	f, err := a.svc.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, files.ErrNotFound) {
		admin.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return nil, false
	}
	return f, true
}

// pageNumber — номер страницы из ?page=; неверный — первая.
func pageNumber(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// ShowPath — карточка файла в админке.
func ShowPath(id string) string { return AdminPath + "/" + id }

func pagePath(n int) string { return AdminPath + "?page=" + strconv.Itoa(n) }

// displayName — имя для подписи: исходное имя файла или имя в адресе.
func displayName(f *files.File) string {
	if f.Name != "" {
		return f.Name
	}
	return f.URLName
}

func formatTime(t time.Time) string { return t.UTC().Format("02.01.2006 15:04 UTC") }

// accept — подсказка выбору файла в браузере; проверяет формат всё равно модуль, по содержимому.
const accept = "application/pdf,video/mp4,video/webm,image/svg+xml,.pdf,.mp4,.webm,.svg"
