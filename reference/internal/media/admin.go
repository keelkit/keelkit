// Package media — медиатека в админке: загрузка изображений в модуль images, сетка превью, карточка
// изображения и удаление. Файлы и метаданные хранит модуль; сайт обращается к ним только через images.Service.
package media

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/keelkit/keelkit/modules/images"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/database"
	"github.com/keelkit/keelkit/reference/internal/web"
)

// AdminPath — раздел медиатеки в админке.
const AdminPath = "/admin/media"

// MaxUploadRequest — предел тела запроса загрузки. Оборачивает маршрут снаружи RequireUser: форма без
// JavaScript передаёт CSRF-токен полем multipart, и RequireUser разбирает тело раньше обработчика.
// Запас над images.MaxBytes нужен, чтобы файл чуть больше лимита дочитался и пользователь получил понятную
// ошибку, а не обрыв соединения.
const MaxUploadRequest = 2*images.MaxBytes + 1<<20

const (
	perPage         = 48      // изображений на странице медиатеки
	multipartMemory = 8 << 20 // часть multipart, которая держится в памяти; остальное — во временном файле
	fileField       = "file"
)

// Admin — раздел «Медиатека». Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	svc   *images.Service
	usage UsageFunc
}

// Usage — где используется изображение: запись контента и ссылка на неё в админке.
type Usage struct {
	Title string
	URL   string
}

// UsageFunc — контент сайта, который ссылается на изображение. Пакет не знает о типах контента:
// функцию собирает cmd/site (imageUsage).
type UsageFunc func(ctx context.Context, imageID string) ([]Usage, error)

// NewAdmin создаёт раздел над сервисом изображений сайта; usage — где используется изображение.
func NewAdmin(svc *images.Service, usage UsageFunc) *Admin { return &Admin{svc: svc, usage: usage} }

// uploadForm — состояние формы загрузки: сообщение об ошибке или пусто.
type uploadForm struct {
	Error string
}

// listView — страница медиатеки.
type listView struct {
	Images   []*images.Image
	Page     int
	HasNext  bool
	Form     uploadForm
	Uploaded bool // показать «Загружено» после редиректа
	Deleted  bool // показать «Удалено» после редиректа
}

// List — GET /admin/media: форма загрузки и сетка изображений от новых к старым, по perPage на странице (?page=N).
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a.renderList(w, r, http.StatusOK, pageNumber(q.Get("page")), uploadForm{}, q.Has("uploaded"), q.Has("deleted"))
}

func (a *Admin) renderList(w http.ResponseWriter, r *http.Request, status, page int, f uploadForm, uploaded, deleted bool) {
	imgs, err := a.svc.List(r.Context(), perPage+1, (page-1)*perPage)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	v := listView{Images: imgs, Page: page, Form: f, Uploaded: uploaded, Deleted: deleted}
	if len(imgs) > perPage {
		v.Images, v.HasNext = imgs[:perPage], true
	}
	web.Render(w, r, status, ListPage(v))
}

// Upload — POST /admin/media: загрузка одного файла. Успех — редирект на медиатеку с «Загружено»;
// ошибка проверки файла — 422: при htmx только форма загрузки, без него — страница медиатеки с ошибкой.
func (a *Admin) Upload(w http.ResponseWriter, r *http.Request) {
	// Тот же лимит, что снаружи (MaxUploadRequest): обработчик не полагается на то, как его смонтировали.
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadRequest)
	msg := ""
	// G120 в gosec v2.29 срабатывает на любой ParseMultipartForm и не учитывает MaxBytesReader выше.
	if err := r.ParseMultipartForm(multipartMemory); err != nil { //nolint:gosec // тело ограничено MaxBytesReader
		msg = "Не удалось прочитать файл: отправьте форму ещё раз."
		if mbe := new(http.MaxBytesError); errors.As(err, &mbe) {
			msg = uploadError(images.ErrTooLarge)
		}
	} else if msg, err = a.upload(r); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if msg == "" {
		admin.Redirect(w, r, AdminPath+"?uploaded=1")
		return
	}
	f := uploadForm{Error: msg}
	if admin.IsHTMX(r) {
		web.Render(w, r, http.StatusUnprocessableEntity, UploadForm(f))
		return
	}
	a.renderList(w, r, http.StatusUnprocessableEntity, 1, f, false, false)
}

// upload загружает файл из уже разобранной формы. Ошибка проверки — сообщение для пользователя, прочие — err.
func (a *Admin) upload(r *http.Request) (msg string, err error) {
	file, hdr, err := r.FormFile(fileField)
	if errors.Is(err, http.ErrMissingFile) {
		return "Выберите файл.", nil
	}
	if err != nil {
		return "Не удалось прочитать файл: отправьте форму ещё раз.", nil
	}
	defer func() { _ = file.Close() }()
	if hdr.Size > images.MaxBytes {
		return uploadError(images.ErrTooLarge), nil
	}
	if _, err := a.svc.Upload(r.Context(), file, hdr.Filename); err != nil {
		if msg := uploadError(err); msg != "" {
			return msg, nil
		}
		return "", err
	}
	return "", nil
}

// uploadError — понятное сообщение для ошибки проверки файла модулем; пусто — ошибка не проверки.
func uploadError(err error) string {
	switch {
	case errors.Is(err, images.ErrTooLarge):
		return fmt.Sprintf("Файл больше %d МБ: уменьшите его и загрузите снова.", images.MaxBytes>>20)
	case errors.Is(err, images.ErrTooManyPixels):
		return fmt.Sprintf("Изображение больше %d мегапикселей: уменьшите его и загрузите снова.", images.MaxPixels/1_000_000)
	case errors.Is(err, images.ErrUnsupportedFormat):
		return "Формат не поддерживается: загрузите JPEG, PNG или WebP (без анимации). HEIC с iPhone сначала сохраните как JPEG."
	case errors.Is(err, images.ErrInvalidImage):
		return "Файл повреждён или не является изображением."
	}
	return ""
}

// Show — GET /admin/media/{id}: изображение, его данные, варианты и где оно используется.
func (a *Admin) Show(w http.ResponseWriter, r *http.Request) {
	img, ok := a.image(w, r)
	if !ok {
		return
	}
	used, err := a.usage(r.Context(), img.ID)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, ShowPage(img, used))
}

// ConfirmDelete — GET /admin/media/{id}/delete: подтверждение удаления (без JavaScript).
// Используемое изображение удалить нельзя — сразу 409 со списком мест, где оно стоит.
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	img, ok := a.image(w, r)
	if !ok {
		return
	}
	used, err := a.usage(r.Context(), img.ID)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if len(used) > 0 {
		web.Render(w, r, http.StatusConflict, DeleteBlockedPage(img, used))
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(img))
}

// Delete — POST /admin/media/{id}/delete. Изображение, на которое ссылается контент сайта (внешний ключ на
// images_files без ON DELETE), модуль не удаляет — 409 с объяснением.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	img, ok := a.image(w, r)
	if !ok {
		return
	}
	err := a.svc.Delete(r.Context(), img.ID)
	switch {
	case err == nil:
		admin.Redirect(w, r, AdminPath+"?deleted=1")
	case errors.Is(err, images.ErrNotFound):
		admin.NotFound(w, r)
	case database.IsForeignKeyViolation(err):
		used, err := a.usage(r.Context(), img.ID)
		if err != nil {
			admin.ServerError(w, r, err)
			return
		}
		web.Render(w, r, http.StatusConflict, DeleteBlockedPage(img, used))
	default:
		admin.ServerError(w, r, err)
	}
}

// image загружает изображение по {id} из пути; нет такого — 404 админки.
func (a *Admin) image(w http.ResponseWriter, r *http.Request) (*images.Image, bool) {
	img, err := a.svc.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, images.ErrNotFound) {
		admin.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return nil, false
	}
	return img, true
}

// pageNumber — номер страницы из ?page=; неверный — первая.
func pageNumber(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

func showPath(id string) string { return AdminPath + "/" + id }

func pagePath(n int) string { return AdminPath + "?page=" + strconv.Itoa(n) }

// displayName — имя для подписи: исходное имя файла или id, если имени нет.
func displayName(img *images.Image) string {
	if img.Name != "" {
		return img.Name
	}
	return img.ID
}

func dimensions(w, h int) string { return strconv.Itoa(w) + "×" + strconv.Itoa(h) }

// formatBytes — размер файла: КБ (с округлением вверх) или МБ с одним знаком.
func formatBytes(n int64) string {
	if n < 1<<20 {
		return strconv.FormatInt((n+1023)>>10, 10) + " КБ"
	}
	return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " МБ"
}

func formatTime(t time.Time) string { return t.UTC().Format("02.01.2006 15:04 UTC") }
