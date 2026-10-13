package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/voffkin/keelkit/modules/auth"
	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/content/categories"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
	"github.com/voffkin/keelkit/reference/internal/content/people"
	"github.com/voffkin/keelkit/reference/internal/content/posts"
	"github.com/voffkin/keelkit/reference/internal/listfield"
	"github.com/voffkin/keelkit/reference/internal/menu"
	"github.com/voffkin/keelkit/reference/internal/seo"
	"github.com/voffkin/keelkit/reference/internal/settings"
)

// seed заполняет БД и загрузки данными для разработки. Идемпотентен: существующие записи не трогает.
func seed(ctx context.Context, cfg config) error {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	media, closeMedia, err := openMedia(db, cfg)
	if err != nil {
		return err
	}
	defer closeMedia()
	imgs, err := seedImages(ctx, media)
	if err != nil {
		return err
	}
	if err := seedContent(ctx, db, imgs); err != nil {
		return err
	}
	// Только для разработки: seed в проде не запускается, поэтому пароль можно напечатать.
	slog.Warn("dev admin (только для разработки)", "url", "/admin", "email", seedAdminEmail, "password", seedAdminPassword)
	slog.Info("seed done")
	return nil
}

// seedContent — настройки сайта, страницы, блог и пользователь админки. imgs — id изображений из seedImages по имени
// файла; блоки с изображением, которого там нет, пропускаются. Отдельно от seedImages: обработка изображений
// занимает около секунды, поэтому тестам, которым картинки не нужны, хватает seedContent(ctx, db, nil).
func seedContent(ctx context.Context, db *sql.DB, imgs map[string]string) error {
	social, err := listfield.Encode([]settings.Social{
		{ID: listfield.NewID(), Name: "Telegram", URL: "https://t.me/example"},
		{ID: listfield.NewID(), Name: "ВКонтакте", URL: "https://vk.com/example"},
	})
	if err != nil {
		return err
	}
	err = settings.New(db).InsertSiteSettingsIfMissing(ctx, settings.InsertSiteSettingsIfMissingParams{
		Name:        "Keelkit — эталонный сайт",
		Description: "Образец устройства сайтов Keelkit",
		Address:     "Москва, ул. Примерная, д. 1\nофис 101",
		Phone:       "+7 (495) 000-00-00",
		Email:       "hello@example.com",
		Social:      social,
		CookieText:  "Сайт использует файлы cookie, чтобы работать удобнее.",
		CookieLink:  "/about",
	})
	if err != nil {
		return err
	}

	for _, p := range seedPages(imgs) {
		if err := pages.SeedPublished(ctx, db, p.title, p.slug, p.blocks); err != nil {
			return fmt.Errorf("seed page %s: %w", p.slug, err)
		}
	}

	for _, p := range seedPeople {
		if _, err := people.Seed(ctx, db, p.name, p.role, imgs[p.photo]); err != nil {
			return fmt.Errorf("seed person %s: %w", p.name, err)
		}
	}

	if err := seedBlog(ctx, db, imgs, time.Now()); err != nil {
		return err
	}
	if err := seedMenus(ctx, db); err != nil {
		return err
	}

	_, err = auth.NewService(db, auth.Config{}).CreateUser(ctx, seedAdminEmail, seedAdminPassword)
	if err != nil && !errors.Is(err, auth.ErrUserExists) {
		return fmt.Errorf("seed user: %w", err)
	}
	return nil
}

// seedImageFiles — изображения для медиатеки (копии из testdata модуля images, см. его README):
// фото на все ширины профиля, прозрачный PNG, маленький JPEG (вариантов шире оригинала нет) и WebP с поворотом из EXIF.
//
//go:embed seeddata
var seedImageFiles embed.FS

// seedImages загружает seedImageFiles через модуль images. Изображение с тем же именем файла уже есть — пропускается.
// Возвращает id изображений по имени файла (и загруженных, и найденных).
func seedImages(ctx context.Context, media *images.Service) (map[string]string, error) {
	existing, err := media.List(ctx, 1000, 0)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(existing))
	for _, img := range existing {
		ids[img.Name] = img.ID
	}
	entries, err := seedImageFiles.ReadDir("seeddata")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if ids[e.Name()] != "" {
			continue
		}
		data, err := seedImageFiles.ReadFile("seeddata/" + e.Name())
		if err != nil {
			return nil, err
		}
		img, err := media.Upload(ctx, bytes.NewReader(data), e.Name())
		if err != nil {
			return nil, fmt.Errorf("seed image %s: %w", e.Name(), err)
		}
		ids[e.Name()] = img.ID
	}
	return ids, nil
}

// Пользователь админки для разработки (seed в проде не запускается). Пароль проходит политику модуля auth:
// от 8 символов.
const (
	seedAdminEmail    = "admin@example.com"
	seedAdminPassword = "adminadmin"
)

// seedPage — страница для seed: заголовок, slug и блоки.
type seedPage struct {
	title, slug string
	blocks      []blocks.Block
}

// seedPages — опубликованные страницы в порядке меню. «О проекте» — все типы блоков (text, image, image_text,
// quote, gallery) и Markdown (таблица, код, ссылки); «Контакты» проверяет санитайзер: скрипт, обработчик события
// и javascript:-ссылка не должны попасть в HTML. imgs — id изображений из seedImages по имени файла:
// без них блоки с изображениями пропускаются.
func seedPages(imgs map[string]string) []seedPage {
	about := []blocks.Block{
		textBlock("about-intro", `Keelkit — набор Go-модулей, спецификаций и скиллов для быстрого создания сайтов:
блогов, новостных сайтов, каталогов и сайтов-визиток.

## Из чего состоит

| Часть | Где | Что внутри |
|---|---|---|
| Модули | `+"`modules/`"+` | изображения, позже — авторизация, SEO, поиск |
| Спецификация | `+"`site.yaml`"+` | типы контента, поля, подключённые модули |
| Эталон | `+"`reference/`"+` | этот сайт |
`),
	}
	if id := imgs["photo.jpg"]; id != "" {
		about = append(about, blocks.Block{ID: "about-photo", Type: "image", Content: &blocks.Image{
			Image: id, Alt: "Тестовая фотография из модуля images", Caption: "Картинка из медиатеки: WebP и JPEG нужной ширины.",
		}})
	}
	if id := imgs["transparent.png"]; id != "" {
		about = append(about, blocks.Block{ID: "about-side", Type: "image_text", Content: &blocks.ImageText{
			Image: id, Alt: "Полупрозрачный рисунок", Caption: "PNG с прозрачностью", Position: blocks.PositionRight,
			Body: `## Как запустить

` + "```sh" + `
make reset && make dev
open http://localhost:9090/
` + "```" + `

Прозрачность сохраняется в WebP; в JPEG фон белый.`,
		}})
	}
	about = append(about,
		textBlock("about-code", `Обработчик страницы на Go выглядит примерно так:

`+"```go"+`
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	p, err := h.q.GetPublishedPageBySlug(r.Context(), r.PathValue("slug"))
	// ...
}
`+"```"+`

Подробнее — в [документации Go](https://go.dev/doc/) и на странице [контактов](/contacts).`),
		blocks.Block{ID: "about-quote", Type: "quote", Content: &blocks.Quote{
			Text: "Писать код дёшево, проверять дорого.", Author: "Принципы Keelkit",
		}},
	)
	if g := seedGallery(imgs); g != nil {
		about = append(about, blocks.Block{ID: "about-gallery", Type: "gallery", Content: g})
	}
	return []seedPage{
		{title: "Добро пожаловать", slug: pages.HomeSlug, blocks: []blocks.Block{textBlock("home-intro", `Это эталонный сайт **Keelkit** — образец, по которому собираются все сайты на общих модулях.

Каждый сайт — самостоятельное Go-приложение: один бинарник, SQLite, шаблоны на templ.
Общие модули подключаются как обычные версионируемые зависимости и обновляются через `+"`go get`"+`.

- [О проекте](/about) — как устроен сайт;
- [Контакты](/contacts) — как с нами связаться.
`)}},
		{title: "О проекте", slug: "about", blocks: about},
		// Страница с блоком-списком: вступление текстом, затем все люди из раздела «Люди».
		{title: "Команда", slug: "team", blocks: []blocks.Block{
			textBlock("team-intro", "Люди, которые делают Keelkit. Порядок задаётся в разделе «Люди» админки."),
			{ID: "team-people", Type: "people", Content: &blocks.Listing{}},
		}},
		{title: "Контакты", slug: "contacts", blocks: []blocks.Block{textBlock("contacts-body", `Пишите на почту: hello@example.com.

Строка ниже проверяет санитайзер: скрипт не должен выполниться и не должен попасть в HTML.

<script>alert(1)</script>

<img src="/static/favicon.svg" alt="" onerror="alert(2)">

[Опасная ссылка](javascript:alert(3))
`)}},
	}
}

// seedGallery — галерея из изображений seed (блок со списком элементов); без изображений — nil.
// small.jpg в seed нигде не используется: на нём тесты проверяют удаление свободного изображения.
func seedGallery(imgs map[string]string) *blocks.Gallery {
	items := []blocks.GalleryItem{
		{ID: "photo", Image: imgs["photo.jpg"], Alt: "Тестовая фотография", Caption: "Фото на все ширины профиля"},
		{ID: "rotated", Image: imgs["orientation-6.webp"], Alt: "Снимок, повёрнутый по EXIF", Caption: "WebP: ориентация из EXIF применена"},
		{ID: "transparent", Image: imgs["transparent.png"], Alt: "Полупрозрачный рисунок"},
	}
	g := &blocks.Gallery{}
	for _, it := range items {
		if it.Image != "" {
			g.Items = append(g.Items, it)
		}
	}
	if len(g.Items) == 0 {
		return nil
	}
	return g
}

// textBlock — блок text для seed. id — читаемые и стабильные: по ним e2e находит блоки (block-<id>).
func textBlock(id, body string) blocks.Block {
	return blocks.Block{ID: id, Type: "text", Content: &blocks.Text{Body: body}}
}

// seedPeople — люди для блока people на странице «Команда», по порядку. Фото — у первого (если есть imgs),
// у остальных нет: на сайте видна заглушка.
var seedPeople = []struct{ name, role, photo string }{
	{"Анна Петрова", "Руководитель проекта", "photo.jpg"},
	{"Борис Иванов", "Разработчик", ""},
	{"Вера Смирнова", "Дизайнер", ""},
}

// seedCategories — категории блога: имя и slug.
var seedCategories = []struct{ name, slug string }{
	{"Go", "go"},
	{"Веб", "web"},
	{"Заметки", "notes"},
}

// Посты seed, которых нет на сайте: черновик и пост с датой публикации в будущем.
const (
	seedDraftPost  = "draft-post"
	seedFuturePost = "future-post"
)

// seedPostTitles — заголовки опубликованных постов, от новых к старым: пост i опубликован i+1 день назад
// (относительно now) и попадает в категорию seedCategories[i%3]. Обложки — у каждого третьего, если есть imgs.
var seedPostTitles = []string{
	"Как устроен эталонный сайт",
	"Черновик-копия вместо флажка",
	"Заметки о SQLite в проде",
	"Блоки вместо Markdown-картинок",
	"Адаптивные изображения без CDN",
	"Почему без Node в сборке",
	"templ: шаблоны, которые проверяет компилятор",
	"htmx для админки",
	"Миграции, которые нельзя откатить",
	"Один бинарник и systemd",
	"Транслитерация для slug",
	"argon2id вместо bcrypt",
	"Первый пост",
}

// seedBlog — категории и посты блога: 13 опубликованных (2 страницы ленты), черновик и пост, запланированный
// на будущее (оба не видны на сайте). imgs — id изображений из seedImages: без них посты без обложек.
func seedBlog(ctx context.Context, db *sql.DB, imgs map[string]string, now time.Time) error {
	cats := make([]int64, len(seedCategories))
	for i, c := range seedCategories {
		id, err := categories.Seed(ctx, db, c.name, c.slug)
		if err != nil {
			return fmt.Errorf("seed category %s: %w", c.slug, err)
		}
		cats[i] = id
	}
	covers := []string{imgs["photo.jpg"], imgs["transparent.png"], imgs["orientation-6.webp"]}
	day := 24 * time.Hour
	now = now.Truncate(time.Minute)

	list := make([]posts.SeedPost, 0, len(seedPostTitles)+2)
	for i, title := range seedPostTitles {
		p := posts.SeedPost{
			Title: title, Slug: fmt.Sprintf("post-%02d", len(seedPostTitles)-i), CategoryID: cats[i%len(cats)],
			PublishDate: now.Add(-time.Duration(i+1) * day), Publish: true,
			Blocks: []blocks.Block{textBlock("intro", seedPostBody(title))},
		}
		if i%3 == 0 {
			p.Cover = covers[(i/3)%len(covers)]
		}
		if i == 0 {
			// Новый пост — со всеми полями: лид, ручной анонс, alt обложки (остальные — с анонсом из текста).
			p.Lead = seedPostLead
			p.Excerpt = seedPostExcerpt
			if p.Cover != "" {
				p.CoverAlt = "Пример фотографии из dev-данных"
			}
		}
		list = append(list, p)
	}
	list = append(list,
		posts.SeedPost{
			Title: "Черновик поста", Slug: seedDraftPost, CategoryID: cats[0],
			Blocks: []blocks.Block{textBlock("intro", "Этот пост ещё не опубликован: на сайте его нет, в админке — «Черновик».")},
		},
		posts.SeedPost{
			Title: "Пост из будущего", Slug: seedFuturePost, CategoryID: cats[2], PublishDate: now.Add(30 * day), Publish: true,
			Blocks: []blocks.Block{textBlock("intro", "Опубликован с датой в будущем: появится на сайте, когда она наступит.")},
		},
	)
	for _, p := range list {
		if err := posts.Seed(ctx, db, p); err != nil {
			return fmt.Errorf("seed post %s: %w", p.Slug, err)
		}
	}
	return nil
}

// Лид и ручной анонс первого поста seed.
const (
	seedPostLead    = "Из чего состоит сайт на Keelkit и почему каждый новый сайт повторяет его устройство."
	seedPostExcerpt = "Анонс, написанный вручную: он заменяет начало текста в ленте, RSS и описании страницы."
)

func seedPostBody(title string) string {
	return "Пост «" + title + "» из dev-данных эталонного сайта. Текст поста собирается из блоков, как и страницы: " +
		"текст, изображение, изображение с текстом, цитата.\n\n" +
		"## Подробнее\n\nАнонс в ленте блога — начало текста блоков, без разметки. Дата публикации и категория — " +
		"под заголовком, обложка — над текстом."
}

// seedMenus — меню, пока они пустые: в шапке — страницы «О проекте» и «Контакты» и блог с категориями
// подпунктами, в подвале — «О проекте», «Контакты» подпунктом и свой адрес (RSS-лента). Так в dev-данных есть
// пункт каждого вида.
func seedMenus(ctx context.Context, db *sql.DB) error {
	pq := pages.New(db)
	page := func(slug string) (string, error) {
		id, err := pq.DraftPageIDBySlug(ctx, slug)
		if err != nil {
			return "", fmt.Errorf("seed menu: page %s: %w", slug, err)
		}
		return menu.PageTarget(id), nil
	}
	about, err := page("about")
	if err != nil {
		return err
	}
	contacts, err := page("contacts")
	if err != nil {
		return err
	}
	menus := map[string][]menu.Item{
		"main": {
			{Target: about},
			{Target: contacts},
			{Target: menu.SectionTarget("blog"), Children: true},
		},
		"footer": {
			{Target: about},
			{Target: contacts, Child: true},
			{Target: menu.URLTarget, Title: "RSS", URL: seo.FeedPath},
		},
	}
	for _, m := range siteMenus {
		if err := menu.SeedIfEmpty(ctx, db, m.Name, menus[m.Name]); err != nil {
			return fmt.Errorf("seed menu %s: %w", m.Name, err)
		}
	}
	return nil
}
