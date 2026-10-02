# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо; этот файл описывает только сайт.

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.1`.
- Модули Keelkit: пока не подключены (`images` — с этапа 8 плана).
- Зависимости: `modernc.org/sqlite` v1.60.1, `github.com/pressly/goose/v3` v3.28.0 (только библиотека, CLI goose не используется),
  `github.com/a-h/templ` v0.3.1020, `github.com/yuin/goldmark` v1.8.6, `github.com/microcosm-cc/bluemonday` v1.0.27,
  `golang.org/x/crypto` v0.57.0 (argon2id), `golang.org/x/term` v0.46.0 (ввод пароля без эха); в тестах — `github.com/PuerkitoBio/goquery` v1.13.0,
  `github.com/chromedp/chromedp` v0.16.0 (только браузерные тесты с тегом `browser`; его `cdproto` — псевдоверсия, теги у неё не ставятся).
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
| `make seed` | dev-данные; идемпотентно, в проде не запускается; печатает dev-пользователя админки |
| `make user-create EMAIL=…` | пользователь админки; пароль спрашивается дважды без эха (не из терминала — строками из stdin) |
| `make reset` | удалить `DATA_DIR` (БД и загрузки); снимки в `backups/` остаются |
| `make generate` | кодогенерация: templ, затем sqlc |
| `make generate-check` | проверить, что генерация ничего не меняет (в CI) |
| `make test` | все тесты, включая e2e |
| `make e2e` | только e2e (`TestE2E*` в `cmd/site`) |
| `make e2e-browser` | браузерные тесты htmx-взаимодействий (`TestBrowser*`, тег `browser`, chromedp; нужен Chrome) |
| `make test-isolated` | тесты с `GOWORK=off`, только на опубликованных версиях модулей |
| `make lint` | golangci-lint, версия закреплена в `Makefile` |

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8080` | адрес HTTP-сервера |
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями |
| `COOKIE_SECURE` | `true` | флаг `Secure` у cookie админки; `false` — только для разработки по http (`make run`/`make dev` передают `false`) |

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — страница из `pages`; `/home` → 301 на `/`.
- `GET /static/…` — файлы из `static/` (embed); ссылки с `?v=<хеш>` кэшируются на год, список каталога не отдаётся.
- `GET /admin/login`, `POST /admin/login`, `POST /admin/logout`, `GET /admin` (дашборд); `/admin/` → 301 на `/admin`;
  прочие пути под `/admin/` — 404 админки, но только после входа (без сессии — редирект на вход).
- Раздел страниц: `GET /admin/pages` (список), `GET /admin/pages/new`, `POST /admin/pages` (создать),
  `GET|POST /admin/pages/{id}` (форма, сохранить), `GET|POST /admin/pages/{id}/delete` (подтверждение, удалить),
  `POST /admin/pages/preview` (htmx: превью Markdown).
- Зарезервированные slug — `reservedSlugs` в `cmd/site/server.go` (`admin`, `static`, `healthz`): их маршруты перекрывают
  страницу, админка такие slug не принимает. Маршрут-поддерево (`/static/`) занимает и путь без слеша (`GET /static` → 404),
  иначе его забрал бы `GET /{slug}`; e2e `TestE2EReservedSlugsShadowPages` проверяет каждый slug из списка.
- Всё остальное — оформленная 404 (`web.NotFound`).
- `GET /healthz` — JSON о состоянии: `{"status":"ok","version":"<версия сборки>","schema":{"site":{"version":3,"pending":0}}}`.

## Бинарник

`site serve | migrate | seed | user create EMAIL`. Миграции применяются только `site migrate`, никогда при старте.

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

## Админка и авторизация

Всё в `internal/admin/` (вынос в модуль — после второго сайта). «Простой режим»: любой пользователь из `users` —
администратор; `users.role` зарезервирована (по умолчанию `admin`, без `CHECK`, чтобы новые роли не требовали пересборки таблицы).

- Пароли — argon2id, формат PHC (`admin.HashPassword`, `admin.VerifyPassword`); параметры — в корневом `AGENTS.md`.
  Хеш со старыми параметрами пересчитывается при входе. Одновременно считается не больше двух хешей (по 64 МиБ).
  Политика для новых паролей — `admin.CheckPasswordPolicy`: от 8 символов до 1024 байт. email хранится в нижнем регистре (`admin.NormalizeEmail`).
- Сессия: cookie `session` со случайным токеном (14 дней), в `sessions` — только SHA-256 токена. При входе всегда новый токен,
  истёкшие сессии удаляются. Cookie: `HttpOnly`, `SameSite=Lax`, `Secure` по `COOKIE_SECURE`.
- Маршруты админки оборачиваются в `newMux`: `adminOnly` (`http.CrossOriginProtection` + данные шапки), защищённые — ещё и
  `signedIn` (`adminH.RequireUser`): без сессии — 303 на `/admin/login?next=…` (для GET), небезопасный метод без CSRF-токена
  сессии — 403. Ответы админки — `Cache-Control: no-store`, страницы — `noindex`.
- CSRF-токен сессии: в формах — скрытое поле `csrf_token` (`admin.CSRFToken(ctx)`), в htmx — заголовок `X-CSRF-Token`
  (layout админки ставит `hx-headers` на `<body>`). Форма входа — double submit cookie `admin_csrf`.
- Вход: 5 неудач за 15 минут для одного email → блокировка до конца окна (429, `Retry-After`); счётчики в памяти процесса.
  Неверный email и неверный пароль дают одно сообщение; для неизвестного email пароль сверяется с фиктивным хешем.
- `next` после входа — только путь внутри `/admin` (`safeNext`), иначе дашборд.
- Пакет не знает о типах контента: счётчики дашборда передаёт `adminCounters` в `cmd/site/server.go` через `admin.Options.Counters`
  (`Counter.URL` — ссылка на раздел), пункты меню — `admin.Options.Sections`. Новый тип контента добавляет туда счётчик и раздел.
- Для разделов типов контента пакет экспортирует: `admin.Layout(title)` (оболочка с меню, текущий пункт — по префиксу пути),
  `admin.CSRFInput()` (скрытое поле токена), `admin.NotFound`, `admin.ServerError`.
- htmx в админке: `<meta name="htmx-config">` разрешает подмену разметки ответом 422 (по умолчанию htmx не подменяет 4xx).
- Dev-пользователь из seed: `admin@example.com` / `admin` (короче политики — seed пишет напрямую и в проде не запускается).

## Тип контента

Один тип — один пакет `internal/content/<type>/` (пример — `pages`):
`queries.sql` + код sqlc, `handlers.go` (`Handler`, `NewHandler(db)`, публичные хендлеры), `views.templ`,
`admin.go` (`Admin`, `NewAdmin(db, …)`, `AdminPath`, хендлеры раздела админки), `admin.templ`.
Миграция — в `db/migrations/`, маршруты — в `newMux`, dev-данные — в `cmd/site/seed.go`, запись sqlc — в `sqlc.yaml`,
счётчик дашборда — в `adminCounters`, пункт меню — в `admin.Options.Sections`.

Раздел админки (образец — `pages/admin.go`):
- Маршруты — `signedIn(...)`: сессия и CSRF уже проверены. Список, `new`, создание `POST /admin/<type>`,
  форма и сохранение `/{id}`, удаление через страницу подтверждения `/{id}/delete` (работает без JavaScript).
- Форма отправляется и обычным POST, и через htmx (`hx-post`, `hx-target="this"`, `hx-swap="outerHTML"`).
  Ошибки валидации — 422: при `HX-Request` только фрагмент формы, без него — страница целиком; введённые данные
  возвращаются в форму, сообщение — у поля (`#<поле>-error`, `aria-invalid`, `aria-describedby`).
- Успех: обычной форме — 303 на форму с `?saved=1` («Сохранено»), htmx — 204 и `HX-Redirect`.
- Нормализация в `validate`: `TrimSpace` заголовка, CRLF → LF в тексте. Уникальность — ограничением `UNIQUE` в БД
  (`database.IsUniqueViolation`), без предварительного `SELECT`: так не проходит и гонка двух запросов.
- slug — `internal/slug`: пустой составляется из заголовка (`slug.Make`, транслитерация кириллицы своей таблицей),
  введённый вручную приводится к тому же виду; затем проверка `reservedSlugs` и уникальности.
- Поле `richtext`: textarea + превью (`hx-post` на `…/preview`, `hx-trigger="input changed delay:400ms"`,
  `hx-sync="this:replace"`); превью при открытии формы рисует сервер, ответ превью — тот же `richtext.Render`.

## Тесты

- Unit-тесты — рядом с пакетом (`internal/richtext`, `internal/database`, `internal/admin`, `internal/slug`).
- e2e — `cmd/site/e2e_test.go`, функции `TestE2E*`: `httptest.NewServer(newMux(db, config{}))` над БД после `migrate` и `seed`,
  HTML разбирается goquery. Хелперы `fetch` (тело уже прочитано и закрыто) и `page` (статус, `Content-Type`, документ).
- e2e админки — `cmd/site/e2e_admin_test.go`: `adminClient` (cookie jar, редиректы не выполняются), `post`, `login`, `loginOK`
  (вход dev-пользователем, возвращает CSRF-токен сессии), `csrfToken` (скрытое поле формы).
- e2e раздела типа контента — `cmd/site/e2e_<type>_admin_test.go` (образец — `pages`): `newPageAdmin` (сайт + вошедший клиент),
  `save`, заголовки `htmx`; каждая ошибка валидации проверяется и без `HX-Request` (страница), и с ним (фрагмент).
- Браузерные тесты — `cmd/site/browser_test.go` (`//go:build browser`, `TestBrowser*`, chromedp, headless Chrome):
  только критичные htmx-взаимодействия — превью при наборе, сохранение через `HX-Redirect`, подмена формы с ошибкой
  без перезагрузки. Запуск — `make e2e-browser`; `make test` их не собирает, линтер — собирает (`build-tags` в `.golangci.yml`).

## Спецификация

`site.yaml` — источник правды о структуре сайта: структурные изменения сначала вносятся в него, затем в код.
Проверка (из корня монорепо): `make spec-validate FILE=reference/site.yaml`.

## Структура

```
cmd/site/main.go            # точка входа: конфиг, подкоманды, источники миграций
cmd/site/server.go          # маршруты, данные шапки (siteLoader), /healthz
cmd/site/seed.go            # dev-данные
cmd/site/user.go            # site user create
cmd/site/e2e_test.go        # e2e (TestE2E*)
cmd/site/e2e_admin_test.go  # e2e админки
cmd/site/e2e_pages_admin_test.go  # e2e раздела «Страницы»
cmd/site/browser_test.go    # браузерные тесты (тег browser, make e2e-browser)
db/migrations/              # миграции сайта (goose) + embed.go
internal/admin/             # админка: вход, сессии, CSRF, ограничение попыток, оболочка, дашборд
internal/content/pages/     # тип контента pages: queries.sql, sqlc, handlers.go, views.templ, admin.go, admin.templ
internal/database/          # открытие SQLite, статус схем, снимок, применение миграций
internal/richtext/          # Markdown → очищенный HTML
internal/slug/              # slug: Make (транслитерация), Valid
internal/settings/          # site_settings: queries.sql и код sqlc
internal/web/               # layout, страницы ошибок, рендер
static/                     # CSS, htmx, иконка + static.go (embed, Path, Handler)
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: новые `internal/content/<type>/`.
