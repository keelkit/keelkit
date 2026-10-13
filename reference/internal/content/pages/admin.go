package pages

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/database"
	"github.com/voffkin/keelkit/reference/internal/seofields"
	"github.com/voffkin/keelkit/reference/internal/slug"
	"github.com/voffkin/keelkit/reference/internal/web"
)

// AdminPath — раздел страниц в админке.
const AdminPath = "/admin/pages"

// BodyBlocks — блоки поля body (site.yaml: pages.body.allow), в порядке кнопок «Добавить блок».
// Что каждый из них есть в internal/blocks и совпадает с site.yaml, проверяет тест.
var BodyBlocks = []string{"text", "image", "image_text", "quote", "gallery"}

// Виды копии содержимого (page_contents.kind).
const (
	kindDraft     = "draft"
	kindPublished = "published"
)

// Ограничения формы.
const (
	maxTitleLen  = 200     // символов
	maxFormSize  = 4 << 20 // байт всего запроса: до блоков.MaxBlocks блоков с Markdown
	libraryLimit = 200     // изображений в выборе у блока: последние загруженные
)

// Media — то, что разделу нужно от модуля images: изображения блоков и выбор из медиатеки.
type Media interface {
	blocks.Images
	List(ctx context.Context, limit, offset int) ([]*images.Image, error)
}

// Usage — что ссылается на страницу (пункт меню): название и ссылка на него в админке.
type Usage struct {
	Title string
	URL   string
}

// UsageFunc — что ссылается на страницу и не даёт её удалить. Пакет не знает о меню: функцию собирает cmd/site
// (pageUsage).
type UsageFunc func(ctx context.Context, pageID int64) ([]Usage, error)

// Admin — раздел «Страницы» в админке: список, создание, редактирование черновика, публикация и снятие
// с публикации, удаление неопубликованных, предпросмотр черновика так, как его покажет сайт.
// Маршруты оборачиваются в admin.RequireUser: сессия и CSRF-токен уже проверены.
type Admin struct {
	db       *sql.DB
	q        *Queries
	media    Media
	mediaURL string
	reserved map[string]bool
	usage    UsageFunc
}

// NewAdmin создаёт раздел. reserved — slug, занятые маршрутами сайта (admin, static, healthz…):
// такая страница была бы не видна, поэтому админка их не принимает. mediaURL — раздел медиатеки
// (ссылка «загрузите в медиатеку» у блоков с изображением). usage — что ссылается на страницу (меню): такую
// страницу не удалить.
func NewAdmin(db *sql.DB, media Media, mediaURL string, reserved []string, usage UsageFunc) *Admin {
	a := &Admin{db: db, q: New(db), media: media, mediaURL: mediaURL, reserved: make(map[string]bool, len(reserved)),
		usage: usage}
	for _, s := range reserved {
		a.reserved[s] = true
	}
	return a
}

// pageForm — форма страницы: поля черновика и сохранённое состояние. ID == 0 — новая страница.
type pageForm struct {
	ID     int64
	Title  string
	Slug   string
	Blocks []blocks.Block
	SEO    seofields.Fields

	// Сохранённое состояние (от него зависят ссылки, статус и кнопки).
	Published     bool   // есть опубликованная копия
	PublishedSlug string // её адрес
	Changed       bool   // черновик отличается от опубликованной копии

	Errors  map[string]string // поле → сообщение; поля блоков — blocks.FieldName
	Env     blocks.FormEnv
	SEOEnv  seofields.Env
	Flash   string // «Сохранено» и т. п. после редиректа
	Unsaved bool   // форму изменила кнопка блока (↑, ↓, «Скрыть»…), но не сохранила
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

// New — GET /admin/pages/new: новая страница с одним текстовым блоком. Пока её не опубликуют, на сайте её нет.
func (a *Admin) New(w http.ResponseWriter, r *http.Request) {
	b, _ := blocks.New("text")
	a.renderForm(w, r, http.StatusOK, pageForm{Blocks: []blocks.Block{b}})
}

// Edit — GET /admin/pages/{id}: форма с черновиком.
func (a *Admin) Edit(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		f.Flash = "Черновик сохранён."
	case q.Has("published"):
		f.Flash = "Опубликовано."
	case q.Has("unpublished"):
		f.Flash = "Страница снята с публикации: на сайте её больше нет, черновик остался."
	}
	a.renderForm(w, r, http.StatusOK, f)
}

// Create — POST /admin/pages.
func (a *Admin) Create(w http.ResponseWriter, r *http.Request) {
	a.submit(w, r, pageForm{})
}

// Update — POST /admin/pages/{id}. Состояние страницы загружается заранее: при ошибке оно нужно форме.
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	saved, ok := a.load(w, r)
	if !ok {
		return
	}
	saved.Title, saved.Slug, saved.Blocks, saved.SEO = "", "", nil, seofields.Fields{}
	a.submit(w, r, saved)
}

// submit обрабатывает отправку формы. Кнопка (поле action): "save" (или пусто — Enter в поле) пишет черновик,
// "publish" пишет черновик и копирует его в опубликованную версию, остальное — действие блока (blocks.Apply):
// форма возвращается с изменением, ничего не сохраняется.
func (a *Admin) submit(w http.ResponseWriter, r *http.Request, f pageForm) {
	if !a.parseForm(w, r, &f) {
		return
	}
	action := r.PostFormValue("action")
	if action != "" && action != "save" && action != "publish" {
		list, ok := blocks.Apply(f.Blocks, action, BodyBlocks)
		if !ok {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		f.Blocks, f.Unsaved = list, true
		a.renderForm(w, r, http.StatusOK, f)
		return
	}

	res, err := blocks.Resolve(r.Context(), a.media, f.Blocks)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Env.Resolved = res
	seoImage, err := f.SEO.Resolve(r.Context(), a.media)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if a.validate(&f, seoImage != nil) {
		publish := action == "publish"
		id, err := a.save(r.Context(), f, publish)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			admin.NotFound(w, r)
			return
		case database.IsUniqueViolation(err):
			f.Errors = map[string]string{"slug": fmt.Sprintf(
				"Страница с адресом «%s» уже есть (опубликованная или черновик): выберите другой slug.", f.Slug)}
		case database.IsForeignKeyViolation(err):
			// Картинку удалили, пока форма была открыта.
			f.Errors = map[string]string{seofields.FieldImage: "Изображение только что удалено: выберите заново."}
		case err != nil:
			admin.ServerError(w, r, err)
			return
		default:
			flag := "?saved=1"
			if publish {
				flag = "?published=1"
			}
			admin.Redirect(w, r, EditPath(id)+flag)
			return
		}
	}
	a.renderForm(w, r, http.StatusUnprocessableEntity, f)
}

// save одной транзакцией пишет черновик и его ссылки на изображения; publish — ещё и копирует черновик
// в опубликованную версию. Новая страница (f.ID == 0) создаётся. Страницы нет — sql.ErrNoRows.
func (a *Admin) save(ctx context.Context, f pageForm, publish bool) (int64, error) {
	data, err := blocks.Marshal(f.Blocks)
	if err != nil {
		return 0, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := a.q.WithTx(tx)

	id := f.ID
	if id == 0 {
		if id, err = q.CreatePage(ctx); err != nil {
			return 0, err
		}
	} else if n, err := q.TouchPage(ctx, id); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, sql.ErrNoRows
	}
	err = q.SaveDraft(ctx, SaveDraftParams{
		PageID: id, Title: f.Title, Slug: f.Slug, Blocks: data,
		SeoTitle: f.SEO.Title, SeoDescription: f.SEO.Description, SeoImage: f.SEO.DBImage(),
	})
	if err != nil {
		return 0, err
	}
	ids := blocks.ImageIDs(f.Blocks)
	if f.SEO.Image != "" && !slices.Contains(ids, f.SEO.Image) {
		ids = append(ids, f.SEO.Image)
	}
	if err := replaceImages(ctx, q, id, kindDraft, ids); err != nil {
		return 0, err
	}
	if publish {
		if err := publishCopy(ctx, q, id); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// replaceImages перезаписывает ссылки копии kind на изображения.
func replaceImages(ctx context.Context, q *Queries, id int64, kind string, ids []string) error {
	if err := q.DeleteContentImages(ctx, DeleteContentImagesParams{PageID: id, Kind: kind}); err != nil {
		return err
	}
	for _, img := range ids {
		if err := q.InsertContentImage(ctx, InsertContentImageParams{PageID: id, Kind: kind, ImageID: img}); err != nil {
			return err
		}
	}
	return nil
}

// publishCopy копирует черновик и его ссылки на изображения в опубликованную копию (внутри транзакции q).
// Прежний опубликованный slug становится редиректом на новый адрес, а опубликованный — перестаёт им быть.
func publishCopy(ctx context.Context, q *Queries, id int64) error {
	if err := q.RememberPublishedSlug(ctx, id); err != nil {
		return err
	}
	if err := q.DeleteContentImages(ctx, DeleteContentImagesParams{PageID: id, Kind: kindPublished}); err != nil {
		return err
	}
	if err := q.PublishDraft(ctx, id); err != nil {
		return err
	}
	if err := q.DropRedirectToPublished(ctx, id); err != nil {
		return err
	}
	return q.PublishDraftImages(ctx, id)
}

// unpublish удаляет опубликованную копию одной транзакцией с записью её slug в редиректы: если запись
// опубликуют снова под другим slug, старый адрес будет вести на новый.
func (a *Admin) unpublish(ctx context.Context, id int64) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := a.q.WithTx(tx)
	if err := q.RememberPublishedSlug(ctx, id); err != nil {
		return err
	}
	if _, err := q.Unpublish(ctx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Unpublish — POST /admin/pages/{id}/unpublish: удалить опубликованную копию, черновик остаётся.
// Главная всегда опубликована: для неё — 409 с объяснением.
func (a *Admin) Unpublish(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if f.PublishedSlug == HomeSlug {
		web.Render(w, r, http.StatusConflict, BlockedPage(f, "Главная страница всегда опубликована: снять её с публикации нельзя."))
		return
	}
	if err := a.unpublish(r.Context(), f.ID); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	admin.Redirect(w, r, EditPath(f.ID)+"?unpublished=1")
}

// ConfirmDelete — GET /admin/pages/{id}/delete: подтверждение удаления (без JavaScript).
// Удалить можно только неопубликованную страницу: опубликованную сначала снимают с публикации (409 с объяснением);
// страницу, на которую ведёт пункт меню, — после того как пункт убран (409 со списком меню).
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if f.Published {
		web.Render(w, r, http.StatusConflict, BlockedPage(f, deleteHint(f.PublishedSlug)))
		return
	}
	used, err := a.usage(r.Context(), f.ID)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if len(used) > 0 {
		web.Render(w, r, http.StatusConflict, UsedPage(f, used))
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(f))
}

// Delete — POST /admin/pages/{id}/delete. Запрос удаляет страницу только без опубликованной копии,
// поэтому страницу, опубликованную между проверкой и удалением, он не тронет; страницу из меню не даст удалить
// внешний ключ menu_items.page_id.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if !f.Published {
		n, err := a.q.DeleteUnpublishedPage(r.Context(), f.ID)
		if database.IsForeignKeyViolation(err) {
			// На страницу ведёт пункт меню (ссылка без ON DELETE): показать, в каком меню.
			used, err := a.usage(r.Context(), f.ID)
			if err != nil {
				admin.ServerError(w, r, err)
				return
			}
			web.Render(w, r, http.StatusConflict, UsedPage(f, used))
			return
		}
		if err != nil {
			admin.ServerError(w, r, err)
			return
		}
		if n > 0 {
			admin.Redirect(w, r, AdminPath+"?deleted=1")
			return
		}
		// Страницу успели опубликовать или удалить: показать актуальное состояние.
		if f, ok = a.load(w, r); !ok {
			return
		}
	}
	web.Render(w, r, http.StatusConflict, BlockedPage(f, deleteHint(f.PublishedSlug)))
}

// View — GET /admin/pages/{id}/view: черновик так, как его покажет сайт после публикации, с плашкой сверху.
func (a *Admin) View(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	v, err := a.viewData(r.Context(), f)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	v.Preview = true
	web.Render(w, r, http.StatusOK, View(v))
}

// SEO — POST /admin/pages/seo (htmx): превью сниппета из присланной формы страницы.
func (a *Admin) SEO(w http.ResponseWriter, r *http.Request) {
	var f pageForm
	if !a.parseForm(w, r, &f) {
		return
	}
	v, err := a.viewData(r.Context(), f)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, seofields.Snippet(viewMeta(v), AdminPath+"/seo"))
}

// viewData — страница из формы так, как её покажет сайт: изображения загружены, пустой slug составлен из заголовка
// (как при сохранении). Нужна предпросмотру и превью сниппета.
func (a *Admin) viewData(ctx context.Context, f pageForm) (viewData, error) {
	res, err := blocks.Resolve(ctx, a.media, f.Blocks)
	if err != nil {
		return viewData{}, err
	}
	img, err := f.SEO.Resolve(ctx, a.media)
	if err != nil {
		return viewData{}, err
	}
	title := strings.TrimSpace(f.Title)
	s := slug.Make(strings.TrimSpace(f.Slug))
	if s == "" {
		s = slug.Make(title)
	}
	return viewData{ID: f.ID, Title: title, Slug: s, Blocks: f.Blocks, Resolved: res, SEO: f.SEO, SEOImage: img}, nil
}

// Preview — POST /admin/pages/preview?field=<имя поля> (htmx): фрагмент с отрисованным Markdown поля.
func (a *Admin) Preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	src := strings.ReplaceAll(r.PostFormValue(r.URL.Query().Get("field")), "\r\n", "\n")
	web.Render(w, r, http.StatusOK, blocks.Preview(src))
}

// Block — POST /admin/pages/block (htmx): фрагмент формы блока. ?type=<тип> — новый пустой блок
// («Добавить блок»), ?id=<id> — блок из присланной формы (перерисовка после выбора изображения).
func (a *Admin) Block(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	var b blocks.Block
	if typ := r.URL.Query().Get("type"); typ != "" {
		if !slices.Contains(BodyBlocks, typ) {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		b, _ = blocks.New(typ)
	} else {
		list, err := blocks.Parse(r.PostForm)
		id := r.URL.Query().Get("id")
		i := slices.IndexFunc(list, func(b blocks.Block) bool { return b.ID == id })
		if err != nil || i < 0 {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		b = list[i]
	}
	env, err := a.env(r.Context(), []blocks.Block{b}, nil)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, blocks.Form(b, env))
}

// load загружает страницу по {id} из пути: черновик в поля формы и сохранённое состояние; нет такой — 404 админки.
func (a *Admin) load(w http.ResponseWriter, r *http.Request) (pageForm, bool) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return pageForm{}, false
	}
	ctx := r.Context()
	draft, err := a.q.GetPageContent(ctx, GetPageContentParams{PageID: id, Kind: kindDraft})
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return pageForm{}, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return pageForm{}, false
	}
	list, err := blocks.Unmarshal(draft.Blocks)
	if err != nil {
		admin.ServerError(w, r, err)
		return pageForm{}, false
	}
	f := pageForm{
		ID: id, Title: draft.Title, Slug: draft.Slug, Blocks: list,
		SEO: seofields.FromDB(draft.SeoTitle, draft.SeoDescription, draft.SeoImage),
	}
	pub, err := a.q.GetPageContent(ctx, GetPageContentParams{PageID: id, Kind: kindPublished})
	switch {
	case err == nil:
		f.Published, f.PublishedSlug = true, pub.Slug
		pub.Kind = draft.Kind
		f.Changed = pub != draft
	case !errors.Is(err, sql.ErrNoRows):
		admin.ServerError(w, r, err)
		return pageForm{}, false
	}
	return f, true
}

func (a *Admin) parseForm(w http.ResponseWriter, r *http.Request, f *pageForm) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return false
	}
	list, err := blocks.Parse(r.PostForm)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return false
	}
	f.Title, f.Slug, f.Blocks = r.PostFormValue("title"), r.PostFormValue("slug"), list
	f.SEO = seofields.Parse(r.PostForm)
	return true
}

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Уникальность slug проверяет БД при сохранении.
// Пустой slug составляется из заголовка; введённый вручную приводится к тому же виду (slug.Make).
// Изображения блоков должны быть уже загружены в f.Env.Resolved; seoImageFound — картинка SEO есть в медиатеке.
func (a *Admin) validate(f *pageForm, seoImageFound bool) bool {
	errs := blocks.Validate(f.Blocks, f.Env.Resolved)
	f.Title = strings.TrimSpace(f.Title)

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
	// Опубликованная главная остаётся главной: иначе сайт открывался бы заглушкой.
	if f.PublishedSlug == HomeSlug && f.Slug != HomeSlug && errs["slug"] == "" {
		errs["slug"] = fmt.Sprintf("Это главная страница: её адрес — «%s».", HomeSlug)
	}
	f.SEO.Validate(errs, seoImageFound)

	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// env — данные формы блоков: выбор изображений из медиатеки, выбранные изображения, адреса htmx.
func (a *Admin) env(ctx context.Context, list []blocks.Block, errs map[string]string) (blocks.FormEnv, error) {
	lib, err := a.media.List(ctx, libraryLimit, 0)
	if err != nil {
		return blocks.FormEnv{}, err
	}
	res, err := blocks.Resolve(ctx, a.media, list)
	if err != nil {
		return blocks.FormEnv{}, err
	}
	return blocks.FormEnv{
		Errors: errs, Resolved: res, Library: lib, Allow: BodyBlocks,
		PreviewURL: AdminPath + "/preview", BlockURL: AdminPath + "/block", MediaURL: a.mediaURL,
	}, nil
}

// renderForm отдаёт форму: при htmx-запросе — только фрагмент формы (его подставит hx-swap), иначе — страницу целиком.
func (a *Admin) renderForm(w http.ResponseWriter, r *http.Request, status int, f pageForm) {
	env, err := a.env(r.Context(), f.Blocks, f.Errors)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Env = env
	v, err := a.viewData(r.Context(), f)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.SEOEnv = seofields.Env{
		Errors: f.Errors, Library: env.Library, Image: v.SEOImage, Meta: viewMeta(v),
		Snippet: AdminPath + "/seo", MediaURL: a.mediaURL,
	}
	c := FormPage(f)
	if admin.IsHTMX(r) {
		c = Form(f)
	}
	web.Render(w, r, status, c)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// EditPath — форма страницы в админке.
func EditPath(id int64) string { return AdminPath + "/" + strconv.FormatInt(id, 10) }

func viewPath(id int64) string { return EditPath(id) + "/view" }

// formatTime — время из БД (UTC RFC 3339) для списка; нераспознанное — как есть.
func formatTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format("02.01.2006 15:04 UTC")
}

// SeedPublished — dev-данные (seed): опубликованная страница из блоков. Страница с таким slug черновика уже есть —
// ничего не делает. Блоки не проверяются: seed пишет их напрямую.
func SeedPublished(ctx context.Context, db *sql.DB, title, slug string, list []blocks.Block) error {
	a := &Admin{db: db, q: New(db)}
	_, err := a.q.DraftPageIDBySlug(ctx, slug)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = a.save(ctx, pageForm{Title: title, Slug: slug, Blocks: list}, true)
	return err
}
