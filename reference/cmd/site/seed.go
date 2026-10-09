package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/voffkin/keelkit/modules/images"

	"github.com/voffkin/keelkit/reference/internal/admin"
	"github.com/voffkin/keelkit/reference/internal/blocks"
	"github.com/voffkin/keelkit/reference/internal/content/pages"
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

// seedContent — настройки сайта, страницы и пользователь админки. imgs — id изображений из seedImages по имени
// файла; блоки с изображением, которого там нет, пропускаются. Отдельно от seedImages: обработка изображений
// занимает около секунды, поэтому тестам, которым картинки не нужны, хватает seedContent(ctx, db, nil).
func seedContent(ctx context.Context, db *sql.DB, imgs map[string]string) error {
	err := settings.New(db).InsertSiteSettingsIfMissing(ctx, settings.InsertSiteSettingsIfMissingParams{
		Name:        "Keelkit — эталонный сайт",
		Description: "Образец устройства сайтов Keelkit",
	})
	if err != nil {
		return err
	}

	for _, p := range seedPages(imgs) {
		if err := pages.SeedPublished(ctx, db, p.title, p.slug, p.blocks); err != nil {
			return fmt.Errorf("seed page %s: %w", p.slug, err)
		}
	}

	hash, err := admin.HashPassword(ctx, seedAdminPassword)
	if err != nil {
		return err
	}
	err = admin.New(db).InsertUserIfMissing(ctx, admin.InsertUserIfMissingParams{Email: seedAdminEmail, PasswordHash: hash})
	if err != nil {
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

// Пользователь админки для разработки. Пароль короче политики CheckPasswordPolicy — это нормально:
// seed пишет в БД напрямую и в проде не запускается.
const (
	seedAdminEmail    = "admin@example.com"
	seedAdminPassword = "admin"
)

// seedPage — страница для seed: заголовок, slug и блоки.
type seedPage struct {
	title, slug string
	blocks      []blocks.Block
}

// seedPages — опубликованные страницы в порядке меню. «О проекте» — все типы блоков (text, image, image_text,
// quote) и Markdown (таблица, код, ссылки); «Контакты» проверяет санитайзер: скрипт, обработчик события
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
			Image: id, Alt: "Полупрозрачный рисунок", Position: blocks.PositionRight,
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
	return []seedPage{
		{title: "Добро пожаловать", slug: pages.HomeSlug, blocks: []blocks.Block{textBlock("home-intro", `Это эталонный сайт **Keelkit** — образец, по которому собираются все сайты на общих модулях.

Каждый сайт — самостоятельное Go-приложение: один бинарник, SQLite, шаблоны на templ.
Общие модули подключаются как обычные версионируемые зависимости и обновляются через `+"`go get`"+`.

- [О проекте](/about) — как устроен сайт;
- [Контакты](/contacts) — как с нами связаться.
`)}},
		{title: "О проекте", slug: "about", blocks: about},
		{title: "Контакты", slug: "contacts", blocks: []blocks.Block{textBlock("contacts-body", `Пишите на почту: hello@example.com.

Строка ниже проверяет санитайзер: скрипт не должен выполниться и не должен попасть в HTML.

<script>alert(1)</script>

<img src="/static/favicon.svg" alt="" onerror="alert(2)">

[Опасная ссылка](javascript:alert(3))
`)}},
	}
}

// textBlock — блок text для seed. id — читаемые и стабильные: по ним e2e находит блоки (block-<id>).
func textBlock(id, body string) blocks.Block {
	return blocks.Block{ID: id, Type: "text", Content: &blocks.Text{Body: body}}
}
