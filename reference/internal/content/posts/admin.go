package posts

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

// AdminPath — раздел постов в админке.
const AdminPath = "/admin/posts"

// BodyBlocks — блоки поля body (site.yaml: posts.body.allow), в порядке кнопок «Добавить блок».
// Что каждый из них есть в internal/blocks и совпадает с site.yaml, проверяет тест.
var BodyBlocks = []string{"text", "image", "image_text", "quote", "gallery"}

// Виды копии содержимого (post_contents.kind).
const (
	kindDraft     = "draft"
	kindPublished = "published"
)

// Ограничения формы.
const (
	maxTitleLen   = 200     // символов
	maxLeadLen    = 500     // символов лида
	maxExcerptLen = 500     // символов ручного анонса
	maxAltLen     = 300     // символов alt обложки
	maxFormSize   = 4 << 20 // байт всего запроса: до blocks.MaxBlocks блоков с Markdown
	libraryLimit  = 200     // изображений в выборе обложки и у блоков: последние загруженные
)

// Media — то, что разделу нужно от модуля images: изображения блоков и обложки, выбор из медиатеки.
type Media interface {
	blocks.Images
	List(ctx context.Context, limit, offset int) ([]*images.Image, error)
}

// Admin — раздел «Посты» в админке: список, создание, редактирование черновика, публикация и снятие
// с публикации, удаление неопубликованных, предпросмотр черновика. Устроен как раздел «Страницы» (pages/admin.go);
// у поста ещё обложка, категория и дата публикации. Маршруты оборачиваются в admin.RequireUser.
type Admin struct {
	db       *sql.DB
	q        *Queries
	media    Media
	mediaURL string
}

// NewAdmin создаёт раздел. mediaURL — раздел медиатеки (ссылка «загрузите в медиатеку»).
func NewAdmin(db *sql.DB, media Media, mediaURL string) *Admin {
	return &Admin{db: db, q: New(db), media: media, mediaURL: mediaURL}
}

// postForm — форма поста: поля черновика и сохранённое состояние. ID == 0 — новый пост.
type postForm struct {
	ID          int64
	Title       string
	Slug        string
	Lead        string // подзаголовок под заголовком; пусто — нет
	Excerpt     string // анонс для ленты, RSS и описания; пусто — начало текста блоков
	Cover       string // id изображения; пусто — без обложки
	CoverAlt    string // alt обложки; пусто — обложка декоративная
	CategoryID  int64  // 0 — без категории
	PublishDate string // как в поле datetime-local (часовой пояс сервера); пусто — «в момент публикации»
	Blocks      []blocks.Block
	SEO         seofields.Fields
	OldPaths    string // поле «Старые адреса»: по пути в строке (oldpaths.go)

	// Сохранённое состояние (от него зависят ссылки, статус и кнопки).
	Published     bool   // есть опубликованная копия
	PublishedSlug string // её адрес
	PublishedDate string // её дата публикации (формат БД)
	Changed       bool   // черновик отличается от опубликованной копии

	Errors     map[string]string // поле → сообщение; поля блоков — blocks.FieldName
	Env        blocks.FormEnv
	SEOEnv     seofields.Env
	Categories []ListCategoryOptionsRow
	Flash      string
	Unsaved    bool // форму изменила кнопка блока (↑, ↓, «Скрыть»…), но не сохранила

	publishDB string   // PublishDate в формате БД после validate
	oldPaths  []string // OldPaths после validate
}

// List — GET /admin/posts.
func (a *Admin) List(w http.ResponseWriter, r *http.Request) {
	list, err := a.q.ListPosts(r.Context())
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, ListAdminPage(list, r.URL.Query().Has("deleted")))
}

// New — GET /admin/posts/new: новый пост с одним текстовым блоком.
func (a *Admin) New(w http.ResponseWriter, r *http.Request) {
	b, _ := blocks.New("text")
	a.renderForm(w, r, http.StatusOK, postForm{Blocks: []blocks.Block{b}})
}

// Edit — GET /admin/posts/{id}: форма с черновиком.
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
		f.Flash = "Пост снят с публикации: на сайте его больше нет, черновик остался."
	}
	a.renderForm(w, r, http.StatusOK, f)
}

// Create — POST /admin/posts.
func (a *Admin) Create(w http.ResponseWriter, r *http.Request) {
	a.submit(w, r, postForm{})
}

// Update — POST /admin/posts/{id}. Состояние поста загружается заранее: при ошибке оно нужно форме.
func (a *Admin) Update(w http.ResponseWriter, r *http.Request) {
	saved, ok := a.load(w, r)
	if !ok {
		return
	}
	saved.Title, saved.Slug, saved.Cover, saved.CategoryID, saved.PublishDate, saved.Blocks = "", "", "", 0, "", nil
	saved.Lead, saved.Excerpt, saved.CoverAlt, saved.SEO, saved.OldPaths = "", "", "", seofields.Fields{}, ""
	a.submit(w, r, saved)
}

// submit обрабатывает отправку формы. Кнопка (поле action): "save" (или пусто) пишет черновик, "publish" — пишет
// и копирует в опубликованную версию, остальное — действие блока (blocks.Apply): форма возвращается с изменением.
func (a *Admin) submit(w http.ResponseWriter, r *http.Request, f postForm) {
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

	ctx := r.Context()
	res, err := blocks.Resolve(ctx, a.media, f.Blocks)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Env.Resolved = res
	cats, err := a.q.ListCategoryOptions(ctx)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Categories = cats
	cover, err := resolveCover(ctx, a.media, f.Cover)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	seoImage, err := f.SEO.Resolve(ctx, a.media)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	ok := validate(&f, cover != nil, seoImage != nil)
	if ok {
		if ok, err = a.checkOldPaths(ctx, &f); err != nil {
			admin.ServerError(w, r, err)
			return
		}
	}
	if ok {
		publish := action == "publish"
		id, err := a.save(ctx, f, publish, time.Now())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			admin.NotFound(w, r)
			return
		case database.IsUniqueViolation(err):
			f.Errors = map[string]string{"slug": fmt.Sprintf(
				"Пост с адресом «%s» уже есть (опубликованный или черновик): выберите другой slug.", f.Slug)}
		case database.IsForeignKeyViolation(err):
			// Категорию или изображение удалили, пока форма была открыта.
			f.Errors = map[string]string{"category": "Категория или изображение только что удалены: выберите заново."}
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

// save одной транзакцией пишет черновик и его ссылки на изображения (обложка и блоки); publish — ещё и копирует
// черновик в опубликованную версию. Пустая дата публикации при публикации становится now (с точностью до минуты,
// как в поле формы) — и в черновике, чтобы копии совпадали. Новый пост (f.ID == 0) создаётся; поста нет — sql.ErrNoRows.
func (a *Admin) save(ctx context.Context, f postForm, publish bool, now time.Time) (int64, error) {
	data, err := blocks.Marshal(f.Blocks)
	if err != nil {
		return 0, err
	}
	if publish && f.publishDB == "" {
		f.publishDB = dbTime(now.Truncate(time.Minute))
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	q := a.q.WithTx(tx)

	id := f.ID
	if id == 0 {
		if id, err = q.CreatePost(ctx); err != nil {
			return 0, err
		}
	} else if n, err := q.TouchPost(ctx, id); err != nil {
		return 0, err
	} else if n == 0 {
		return 0, sql.ErrNoRows
	}
	err = q.SaveDraft(ctx, SaveDraftParams{
		PostID: id, Title: f.Title, Slug: f.Slug,
		Cover:       sql.NullString{String: f.Cover, Valid: f.Cover != ""},
		CategoryID:  sql.NullInt64{Int64: f.CategoryID, Valid: f.CategoryID != 0},
		PublishDate: f.publishDB, Blocks: data,
		Lead: f.Lead, Excerpt: f.Excerpt, CoverAlt: f.CoverAlt,
		SeoTitle: f.SEO.Title, SeoDescription: f.SEO.Description, SeoImage: f.SEO.DBImage(),
	})
	if err != nil {
		return 0, err
	}
	ids := blocks.ImageIDs(f.Blocks)
	for _, img := range []string{f.Cover, f.SEO.Image} {
		if img != "" && !slices.Contains(ids, img) {
			ids = append(ids, img)
		}
	}
	if err := replaceImages(ctx, q, id, kindDraft, ids); err != nil {
		return 0, err
	}
	// До публикации: её прежний slug добавится к старым адресам из формы.
	if err := replaceOldPaths(ctx, q, id, f.oldPaths); err != nil {
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
	if err := q.DeleteContentImages(ctx, DeleteContentImagesParams{PostID: id, Kind: kind}); err != nil {
		return err
	}
	for _, img := range ids {
		if err := q.InsertContentImage(ctx, InsertContentImageParams{PostID: id, Kind: kind, ImageID: img}); err != nil {
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
	if err := q.DeleteContentImages(ctx, DeleteContentImagesParams{PostID: id, Kind: kindPublished}); err != nil {
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

// Unpublish — POST /admin/posts/{id}/unpublish: удалить опубликованную копию, черновик остаётся.
func (a *Admin) Unpublish(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if err := a.unpublish(r.Context(), f.ID); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	admin.Redirect(w, r, EditPath(f.ID)+"?unpublished=1")
}

// ConfirmDelete — GET /admin/posts/{id}/delete: подтверждение удаления. Опубликованный пост сначала снимают
// с публикации (409 с объяснением).
func (a *Admin) ConfirmDelete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if f.Published {
		web.Render(w, r, http.StatusConflict, BlockedPage(f))
		return
	}
	web.Render(w, r, http.StatusOK, DeletePage(f))
}

// Delete — POST /admin/posts/{id}/delete. Запрос удаляет пост только без опубликованной копии.
func (a *Admin) Delete(w http.ResponseWriter, r *http.Request) {
	f, ok := a.load(w, r)
	if !ok {
		return
	}
	if !f.Published {
		n, err := a.q.DeleteUnpublishedPost(r.Context(), f.ID)
		if err != nil {
			admin.ServerError(w, r, err)
			return
		}
		if n > 0 {
			admin.Redirect(w, r, AdminPath+"?deleted=1")
			return
		}
		// Пост успели опубликовать или удалить: показать актуальное состояние.
		if f, ok = a.load(w, r); !ok {
			return
		}
	}
	web.Render(w, r, http.StatusConflict, BlockedPage(f))
}

// View — GET /admin/posts/{id}/view: черновик так, как его покажет сайт после публикации, с плашкой сверху.
func (a *Admin) View(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return
	}
	ctx := r.Context()
	d, err := a.q.GetPostContent(ctx, GetPostContentParams{PostID: id, Kind: kindDraft})
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	e := entry{
		PostID: d.PostID, Title: d.Title, Slug: d.Slug, Cover: d.Cover, PublishDate: d.PublishDate, Blocks: d.Blocks,
		Lead: d.Lead, Excerpt: d.Excerpt, CoverAlt: d.CoverAlt,
		SeoTitle: d.SeoTitle, SeoDescription: d.SeoDescription, SeoImage: d.SeoImage,
	}
	if d.CategoryID.Valid {
		cats, err := a.q.ListCategoryOptions(ctx)
		if err != nil {
			admin.ServerError(w, r, err)
			return
		}
		if i := slices.IndexFunc(cats, func(c ListCategoryOptionsRow) bool { return c.ID == d.CategoryID.Int64 }); i >= 0 {
			e.CategoryName = cats[i].Name
			e.CategorySlug = cats[i].Slug
		}
	}
	v, err := newViewData(ctx, a.media, e)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	v.Preview = true
	web.Render(w, r, http.StatusOK, View(v))
}

// SEO — POST /admin/posts/seo (htmx): превью сниппета из присланной формы поста.
func (a *Admin) SEO(w http.ResponseWriter, r *http.Request) {
	var f postForm
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

// viewData — пост из формы так, как его покажет сайт (для превью сниппета): изображения загружены, пустой slug
// составлен из заголовка, как при сохранении. Категория и дата в сниппет не попадают и не загружаются.
func (a *Admin) viewData(ctx context.Context, f postForm) (viewData, error) {
	data, err := blocks.Marshal(f.Blocks)
	if err != nil {
		return viewData{}, err
	}
	title := strings.TrimSpace(f.Title)
	s := slug.Make(strings.TrimSpace(f.Slug))
	if s == "" {
		s = slug.Make(title)
	}
	return newViewData(ctx, a.media, entry{
		PostID: f.ID, Title: title, Slug: s, Blocks: data,
		Cover:    sql.NullString{String: strings.TrimSpace(f.Cover), Valid: strings.TrimSpace(f.Cover) != ""},
		Excerpt:  plainText(f.Excerpt),
		CoverAlt: strings.TrimSpace(f.CoverAlt),
		SeoTitle: f.SEO.Title, SeoDescription: f.SEO.Description, SeoImage: f.SEO.DBImage(),
	})
}

// Preview — POST /admin/posts/preview?field=<имя поля> (htmx): фрагмент с отрисованным Markdown поля.
func (a *Admin) Preview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	src := strings.ReplaceAll(r.PostFormValue(r.URL.Query().Get("field")), "\r\n", "\n")
	web.Render(w, r, http.StatusOK, blocks.Preview(src))
}

// Block — POST /admin/posts/block (htmx): фрагмент формы блока — нового (?type=) или из присланной формы (?id=).
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

// Cover — POST /admin/posts/cover (htmx): поле обложки с превью выбранного изображения (поле cover из формы).
func (a *Admin) Cover(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormSize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	f := postForm{Cover: strings.TrimSpace(r.PostFormValue("cover")), CoverAlt: r.PostFormValue("cover_alt")}
	env, err := a.env(r.Context(), nil, nil)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Env = env
	if err := a.resolveCover(r.Context(), &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	web.Render(w, r, http.StatusOK, coverField(f))
}

// load загружает пост по {id} из пути: черновик в поля формы и сохранённое состояние; нет такого — 404 админки.
func (a *Admin) load(w http.ResponseWriter, r *http.Request) (postForm, bool) {
	id, ok := pathID(r)
	if !ok {
		admin.NotFound(w, r)
		return postForm{}, false
	}
	ctx := r.Context()
	draft, err := a.q.GetPostContent(ctx, GetPostContentParams{PostID: id, Kind: kindDraft})
	if errors.Is(err, sql.ErrNoRows) {
		admin.NotFound(w, r)
		return postForm{}, false
	}
	if err != nil {
		admin.ServerError(w, r, err)
		return postForm{}, false
	}
	list, err := blocks.Unmarshal(draft.Blocks)
	if err != nil {
		admin.ServerError(w, r, err)
		return postForm{}, false
	}
	f := postForm{
		ID: id, Title: draft.Title, Slug: draft.Slug, Cover: draft.Cover.String, CategoryID: draft.CategoryID.Int64,
		PublishDate: inputValue(draft.PublishDate), Blocks: list,
		Lead: draft.Lead, Excerpt: draft.Excerpt, CoverAlt: draft.CoverAlt,
		SEO: seofields.FromDB(draft.SeoTitle, draft.SeoDescription, draft.SeoImage),
	}
	old, err := a.q.ListOldPaths(ctx, id)
	if err != nil {
		admin.ServerError(w, r, err)
		return postForm{}, false
	}
	f.OldPaths = strings.Join(old, "\n")
	pub, err := a.q.GetPostContent(ctx, GetPostContentParams{PostID: id, Kind: kindPublished})
	switch {
	case err == nil:
		f.Published, f.PublishedSlug, f.PublishedDate = true, pub.Slug, pub.PublishDate
		pub.Kind = draft.Kind
		f.Changed = pub != draft
	case !errors.Is(err, sql.ErrNoRows):
		admin.ServerError(w, r, err)
		return postForm{}, false
	}
	return f, true
}

func (a *Admin) parseForm(w http.ResponseWriter, r *http.Request, f *postForm) bool {
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
	f.Cover, f.PublishDate = strings.TrimSpace(r.PostFormValue("cover")), strings.TrimSpace(r.PostFormValue("publish_date"))
	f.Lead, f.Excerpt, f.CoverAlt = r.PostFormValue("lead"), r.PostFormValue("excerpt"), r.PostFormValue("cover_alt")
	f.SEO = seofields.Parse(r.PostForm)
	f.OldPaths = r.PostFormValue("old_paths")
	if s := r.PostFormValue("category"); s != "" {
		if f.CategoryID, err = strconv.ParseInt(s, 10, 64); err != nil || f.CategoryID <= 0 {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return false
		}
	}
	return true
}

// validate нормализует поля и проверяет их; ошибки — в f.Errors. Уникальность slug проверяет БД при сохранении.
// Изображения блоков должны быть уже загружены в f.Env.Resolved, категории — в f.Categories; coverFound —
// выбранная обложка есть в медиатеке, seoImageFound — картинка SEO.
func validate(f *postForm, coverFound, seoImageFound bool) bool {
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

	f.Lead = plainText(f.Lead)
	if utf8.RuneCountInString(f.Lead) > maxLeadLen {
		errs["lead"] = fmt.Sprintf("Лид длиннее %d символов.", maxLeadLen)
	}
	f.Excerpt = plainText(f.Excerpt)
	if utf8.RuneCountInString(f.Excerpt) > maxExcerptLen {
		errs["excerpt"] = fmt.Sprintf("Анонс длиннее %d символов.", maxExcerptLen)
	}

	if f.Cover != "" && !coverFound {
		errs["cover"] = "Изображения нет в медиатеке: выберите другое."
	}
	// alt без обложки ничего не описывает: убираем, чтобы он не появился у следующей выбранной картинки.
	if f.CoverAlt = strings.TrimSpace(f.CoverAlt); f.Cover == "" {
		f.CoverAlt = ""
	}
	if utf8.RuneCountInString(f.CoverAlt) > maxAltLen {
		errs["cover_alt"] = fmt.Sprintf("Описание обложки длиннее %d символов.", maxAltLen)
	}
	f.SEO.Validate(errs, seoImageFound)
	if f.CategoryID != 0 && !slices.ContainsFunc(f.Categories, func(c ListCategoryOptionsRow) bool { return c.ID == f.CategoryID }) {
		errs["category"] = "Такой категории нет: выберите другую."
	}

	var msg string
	if f.oldPaths, msg = parseOldPaths(f.OldPaths); msg != "" {
		errs["old_paths"] = msg
	}

	f.publishDB = ""
	if f.PublishDate != "" {
		var ok bool
		if f.publishDB, ok = parseInput(f.PublishDate); !ok {
			errs["publish_date"] = "Укажите дату и время публикации или оставьте поле пустым."
		}
	}

	if len(errs) > 0 {
		f.Errors = errs
		return false
	}
	return true
}

// plainText — поле простого текста (лид, анонс): без пробелов по краям, CRLF → LF.
func plainText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
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

// resolveCover добавляет выбранную обложку в f.Env.Resolved (её превью и пункт в выборе, если она загружена давно).
func (a *Admin) resolveCover(ctx context.Context, f *postForm) error {
	img, err := resolveCover(ctx, a.media, f.Cover)
	if err != nil || img == nil {
		return err
	}
	if f.Env.Resolved == nil {
		f.Env.Resolved = blocks.Resolved{}
	}
	f.Env.Resolved[img.ID] = img
	return nil
}

// renderForm отдаёт форму: при htmx-запросе — только фрагмент формы, иначе — страницу целиком.
func (a *Admin) renderForm(w http.ResponseWriter, r *http.Request, status int, f postForm) {
	ctx := r.Context()
	env, err := a.env(ctx, f.Blocks, f.Errors)
	if err != nil {
		admin.ServerError(w, r, err)
		return
	}
	f.Env = env
	if err := a.resolveCover(ctx, &f); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	if f.Categories, err = a.q.ListCategoryOptions(ctx); err != nil {
		admin.ServerError(w, r, err)
		return
	}
	v, err := a.viewData(ctx, f)
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

// EditPath — форма поста в админке.
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

// SeedPost — пост для dev-данных.
type SeedPost struct {
	Title, Slug string
	Lead        string
	Excerpt     string // пусто — анонс из текста блоков
	Cover       string // id изображения или пусто
	CoverAlt    string
	CategoryID  int64 // 0 — без категории
	PublishDate time.Time
	Blocks      []blocks.Block
	Publish     bool     // false — только черновик
	OldPaths    []string // старые адреса (путь после BlogPath/), как из формы после проверки
}

// Seed — dev-данные (seed): пост; пост с таким slug черновика уже есть — ничего не делает.
// Поля не проверяются: seed пишет их напрямую.
func Seed(ctx context.Context, db *sql.DB, p SeedPost) error {
	a := &Admin{db: db, q: New(db)}
	_, err := a.q.DraftPostIDBySlug(ctx, p.Slug)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	f := postForm{
		Title: p.Title, Slug: p.Slug, Lead: p.Lead, Excerpt: p.Excerpt, Cover: p.Cover, CoverAlt: p.CoverAlt,
		CategoryID: p.CategoryID, Blocks: p.Blocks, oldPaths: p.OldPaths,
	}
	if !p.PublishDate.IsZero() {
		f.publishDB = dbTime(p.PublishDate)
	}
	_, err = a.save(ctx, f, p.Publish, time.Now())
	return err
}
