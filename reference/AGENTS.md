# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо; этот файл описывает только сайт.

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.1`.
- Модули Keelkit: пока не подключены (`images` — с этапа 8 плана).
- Зависимости: `modernc.org/sqlite` v1.60.1, `github.com/pressly/goose/v3` v3.28.0 (только библиотека, CLI goose не используется),
  `github.com/a-h/templ` v0.3.1020, `github.com/yuin/goldmark` v1.8.6, `github.com/microcosm-cc/bluemonday` v1.0.27;
  в тестах — `github.com/PuerkitoBio/goquery` v1.13.0.
- Инструменты (версии закреплены в `Makefile`): sqlc v1.31.1; CLI templ — `TEMPL_VERSION`, обязан совпадать с библиотекой
  в `go.mod` (`make generate` проверяет это и откажется запускаться при расхождении).
- Вендоренный JS: htmx 2.0.11 — `static/htmx-2.0.11.min.js` (сверен с sha512 из npm).

## Команды

Все команды — через `make` (список: `make help`).

| Цель | Что делает |
|---|---|
| `make run` | запустить сайт (`ADDR`, `DATA_DIR`); при непримененных миграциях отказывается стартовать |
| `make dev` | для разработки: `migrate`, `seed`, запуск |
| `make migrate` | снимок БД в `BACKUP_DIR` (`VACUUM INTO`), затем миграции: модули в порядке `site.yaml`, потом сайт |
| `make seed` | dev-данные; идемпотентно, в проде не запускается |
| `make reset` | удалить `DATA_DIR` (БД и загрузки); снимки в `backups/` остаются |
| `make generate` | кодогенерация: templ, затем sqlc |
| `make generate-check` | проверить, что генерация ничего не меняет (в CI) |
| `make test` | все тесты, включая e2e |
| `make e2e` | только e2e (`TestE2E*` в `cmd/site`) |
| `make test-isolated` | тесты с `GOWORK=off`, только на опубликованных версиях модулей |
| `make lint` | golangci-lint, версия закреплена в `Makefile` |

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8080` | адрес HTTP-сервера |
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями |

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — страница из `pages`; `/home` → 301 на `/`.
- `GET /static/…` — файлы из `static/` (embed); ссылки с `?v=<хеш>` кэшируются на год, список каталога не отдаётся.
- Всё остальное — оформленная 404 (`web.NotFound`).
- `GET /healthz` — JSON о состоянии: `{"status":"ok","version":"<версия сборки>","schema":{"site":{"version":2,"pending":0}}}`.

## Бинарник

`site serve | migrate | seed`. Миграции применяются только `site migrate`, никогда при старте.

## БД и миграции

- SQLite (`DATA_DIR/site.db`), прагмы: WAL, `foreign_keys`, `busy_timeout`.
- Миграции сайта — `db/migrations/NNNNN_<имя>.sql` (goose, нумерация последовательная), встраиваются через `embed`;
  таблица версий `goose_site_version`. Только `-- +goose Up`, down-секции не пишем.
- Порядок источников миграций — `migrationSources` в `cmd/site/main.go`: модули в порядке `site.yaml`, последним — сайт.
- `/healthz` и проверка при старте читают состояние схем, ничего не записывая в БД.
- sqlc: схема — `db/migrations`, запросы — `queries.sql` рядом с пакетом, конфиг — `sqlc.yaml` (по записи на пакет);
  сгенерированный код коммитится. Комментарий перед первым `-- name:` sqlc приклеит к первому запросу — заголовков в файле не пишем.
- Таблица типа контента: `id INTEGER PRIMARY KEY`, поля из `site.yaml`, `created_at`/`updated_at` (TEXT, UTC RFC 3339), `STRICT`.
  Необязательные текстовые поля — `NOT NULL DEFAULT ''` (без `sql.NullString`), обязательные — `CHECK (поле <> '')`, `slug` — `UNIQUE`.
- **`queries.sql` — только ASCII**, включая комментарии: sqlc v1.31.1 для SQLite вырезает текст запроса по смещениям
  в байтах, и кириллица в файле обрезает запросы. Русские тексты передаются параметрами. В миграциях кириллица допустима.

## Шаблоны и HTML

- Шаблоны — templ (`*.templ` рядом с кодом, `*_templ.go` генерируются и коммитятся).
- `internal/web`: `Layout(title)` — оболочка страницы; `NotFoundPage`, `ErrorPage`; `Render` (рендер в буфер, затем ответ),
  `NotFound`, `ServerError` (логирует, клиенту — страница 500 без подробностей).
- Данные шапки (`web.Site`: название, описание, меню) собирает `siteLoader` в `cmd/site/server.go`; middleware `web.WithSite`
  кладёт их в контекст, layout читает через `web.SiteFrom(ctx)`. HTML-маршруты оборачиваются в него, `/healthz` и `/static/` — нет.
  `internal/web` не импортирует типы контента: связывает их только `cmd/site`.
- `richtext` (Markdown): `richtext.Render` → goldmark (GFM, сырой HTML не пропускается) → bluemonday (UGC; `class="language-*"`
  у `code`; `nofollow` только у внешних ссылок). Результат — тип `richtext.HTML`, в шаблоне выводится как `@body`.
  `templ.Raw` для пользовательского контента не используем.
- Статика — `static/` (пакет `static`, embed). В шаблоне ссылка только через `static.Path("имя")`: она добавляет `?v=<хеш>`,
  а неизвестное имя паникует (ловится e2e). Вендоренные библиотеки — с версией в имени файла.
- Стили — `static/style.css`, чистый CSS с переменными; тёмная тема по `prefers-color-scheme`.

## Тип контента

Один тип — один пакет `internal/content/<type>/` (пример — `pages`):
`queries.sql` + код sqlc, `handlers.go` (`Handler`, `NewHandler(db)`, методы-хендлеры), `views.templ`.
Миграция — в `db/migrations/`, маршруты — в `newMux`, dev-данные — в `cmd/site/seed.go`, запись sqlc — в `sqlc.yaml`.

## Тесты

- Unit-тесты — рядом с пакетом (`internal/richtext`, `internal/database`).
- e2e — `cmd/site/e2e_test.go`, функции `TestE2E*`: `httptest.NewServer(newMux(db))` над БД после `migrate` и `seed`,
  HTML разбирается goquery. Хелперы `fetch` (тело уже прочитано и закрыто) и `page` (статус, `Content-Type`, документ).

## Спецификация

`site.yaml` — источник правды о структуре сайта: структурные изменения сначала вносятся в него, затем в код.
Проверка (из корня монорепо): `make spec-validate FILE=reference/site.yaml`.

## Структура

```
cmd/site/main.go            # точка входа: конфиг, подкоманды, источники миграций
cmd/site/server.go          # маршруты, данные шапки (siteLoader), /healthz
cmd/site/seed.go            # dev-данные
cmd/site/e2e_test.go        # e2e (TestE2E*)
db/migrations/              # миграции сайта (goose) + embed.go
internal/content/pages/     # тип контента pages: queries.sql, sqlc, handlers.go, views.templ
internal/database/          # открытие SQLite, статус схем, снимок, применение миграций
internal/richtext/          # Markdown → очищенный HTML
internal/settings/          # site_settings: queries.sql и код sqlc
internal/web/               # layout, страницы ошибок, рендер
static/                     # CSS, htmx, иконка + static.go (embed, Path, Handler)
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: `internal/admin/` (этап 4), новые `internal/content/<type>/`.
