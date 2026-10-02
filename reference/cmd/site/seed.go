package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/keelkit/keelkit/reference/internal/admin"
	"github.com/keelkit/keelkit/reference/internal/content/pages"
	"github.com/keelkit/keelkit/reference/internal/settings"
)

// seed заполняет БД данными для разработки. Идемпотентен: существующие записи не трогает.
func seed(ctx context.Context, cfg config) error {
	db, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	err = settings.New(db).InsertSiteSettingsIfMissing(ctx, settings.InsertSiteSettingsIfMissingParams{
		Name:        "Keelkit — эталонный сайт",
		Description: "Образец устройства сайтов Keelkit",
	})
	if err != nil {
		return err
	}

	pq := pages.New(db)
	for _, p := range seedPages {
		if err := pq.InsertPageIfMissing(ctx, p); err != nil {
			return fmt.Errorf("seed page %s: %w", p.Slug, err)
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
	// Только для разработки: seed в проде не запускается, поэтому пароль можно напечатать.
	slog.Warn("dev admin (только для разработки)", "url", "/admin", "email", seedAdminEmail, "password", seedAdminPassword)
	slog.Info("seed done")
	return nil
}

// Пользователь админки для разработки. Пароль короче политики CheckPasswordPolicy — это нормально:
// seed пишет в БД напрямую и в проде не запускается.
const (
	seedAdminEmail    = "admin@example.com"
	seedAdminPassword = "admin"
)

// seedPages — страницы в порядке меню. «О проекте» проверяет Markdown (таблица, код, ссылки),
// «Контакты» — санитайзер: скрипт, обработчик события и javascript:-ссылка не должны попасть в HTML.
var seedPages = []pages.InsertPageIfMissingParams{
	{
		Title: "Добро пожаловать",
		Slug:  pages.HomeSlug,
		Body: `Это эталонный сайт **Keelkit** — образец, по которому собираются все сайты на общих модулях.

Каждый сайт — самостоятельное Go-приложение: один бинарник, SQLite, шаблоны на templ.
Общие модули подключаются как обычные версионируемые зависимости и обновляются через ` + "`go get`" + `.

- [О проекте](/about) — как устроен сайт;
- [Контакты](/contacts) — как с нами связаться.
`,
	},
	{
		Title: "О проекте",
		Slug:  "about",
		Body: `Keelkit — набор Go-модулей, спецификаций и скиллов для быстрого создания сайтов:
блогов, новостных сайтов, каталогов и сайтов-визиток.

## Из чего состоит

| Часть | Где | Что внутри |
|---|---|---|
| Модули | ` + "`modules/`" + ` | изображения, позже — авторизация, SEO, поиск |
| Спецификация | ` + "`site.yaml`" + ` | типы контента, поля, подключённые модули |
| Эталон | ` + "`reference/`" + ` | этот сайт |

## Как запустить

` + "```sh" + `
make reset && make dev
open http://localhost:8080/
` + "```" + `

Обработчик страницы на Go выглядит примерно так:

` + "```go" + `
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	p, err := h.q.GetPageBySlug(r.Context(), r.PathValue("slug"))
	// ...
}
` + "```" + `

> Писать код дёшево, проверять дорого.

Подробнее — в [документации Go](https://go.dev/doc/) и на странице [контактов](/contacts).
`,
	},
	{
		Title: "Контакты",
		Slug:  "contacts",
		Body: `Пишите на почту: hello@example.com.

Строка ниже проверяет санитайзер: скрипт не должен выполниться и не должен попасть в HTML.

<script>alert(1)</script>

<img src="/static/favicon.svg" alt="" onerror="alert(2)">

[Опасная ссылка](javascript:alert(3))
`,
	},
}
