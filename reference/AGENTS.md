# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо; этот файл описывает только сайт.

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.1`.
- Модули Keelkit: `github.com/voffkin/keelkit/modules/images` v0.1.0 (в `site.yaml` — `modules: { images: v0 }`).
  Его зависимости приходят транзитивно: `golang.org/x/image`, `github.com/gen2brain/webp` (+ `ebitengine/purego`).
- Тег сборки `nodynamic` (`BUILD_TAGS` в `Makefile`, экспортируется через `GOFLAGS`): кодек WebP не подгружает
  системную libwebp, а всегда работает на коде, транслированном в Go. Обработка изображений даёт одинаковый результат
  на любой машине, бинарник не зависит от библиотек хоста. Сборка и тесты без `make` — с `-tags nodynamic`.
- Зависимости: `modernc.org/sqlite` v1.60.1, `github.com/pressly/goose/v3` v3.28.0 (только библиотека, CLI goose не используется),
  `github.com/a-h/templ` v0.3.1020, `github.com/yuin/goldmark` v1.8.6, `github.com/microcosm-cc/bluemonday` v1.0.27,
  `golang.org/x/crypto` v0.57.0 (argon2id), `golang.org/x/term` v0.46.0 (ввод пароля без эха); в тестах — `github.com/PuerkitoBio/goquery` v1.13.0,
  `github.com/chromedp/chromedp` v0.16.0 (только браузерные тесты с тегом `browser`; его `cdproto` — псевдоверсия, теги у неё не ставятся).
- Инструменты (версии закреплены в `Makefile`): sqlc v1.31.1; CLI templ — `TEMPL_VERSION`, обязан совпадать с библиотекой
  в `go.mod` (`make generate` проверяет это и откажется запускаться при расхождении).
- Вендоренный JS: htmx 2.0.11 — `static/htmx-2.0.11.min.js` (сверен с sha512 из npm).

## Доступ к модулям Keelkit

Модули Keelkit лежат в приватном репозитории `github.com/voffkin/keelkit`. `Makefile` экспортирует `GOPRIVATE`
и правило git `insteadOf` (через `GIT_CONFIG_*`), поэтому `make` скачивает модули по SSH, не трогая глобальные
настройки git и Go. Нужен только SSH-ключ с доступом к репозиторию: `ssh -T git@github.com` должен поздороваться.
Для gopls в IDE (запускается вне `make`) — один раз `go env -w 'GOPRIVATE=github.com/voffkin/*'`. В CI — deploy key только для чтения.

## Команды

Все команды — через `make` (список: `make help`).

| Цель | Что делает |
|---|---|
| `make run` | запустить сайт (`ADDR`, `DATA_DIR`); при непримененных миграциях отказывается стартовать |
| `make dev` | для разработки: `migrate`, `seed`, запуск |
| `make migrate` | снимок БД в `BACKUP_DIR` (`VACUUM INTO`), затем миграции: модули в порядке `site.yaml`, потом сайт |
| `make seed` | dev-данные; идемпотентно, в проде не запускается; печатает dev-пользователя админки |
| `make reprocess-images BELOW=N` | повторная обработка изображений с версией профиля ниже `N` текущим профилем; идемпотентна и безопасна для прерывания (повторный запуск доделывает), сервер может работать. Печатает `processed`/`failed` |
| `make user-create EMAIL=…` | пользователь админки; пароль спрашивается дважды без эха (не из терминала — строками из stdin). Ввод не виден — проверьте раскладку: пароль, дважды набранный кириллицей, совпадёт сам с собой, но не с латиницей в браузере. Смены пароля пока нет |
| `make reset` | удалить `DATA_DIR` (БД и загрузки); снимки в `backups/` остаются. Сначала остановите сервер: запущенный процесс продолжит работать с удалённой БД, а новый `make dev` не займёт порт |
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
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками (`uploads/`: оригиналы и варианты изображений) |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями |
| `COOKIE_SECURE` | `true` | флаг `Secure` у cookie админки; `false` — только для разработки по http (`make run`/`make dev` передают `false`) |

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — страница из `pages`; `/home` → 301 на `/`.
- `GET /static/…` — файлы из `static/` (embed); ссылки с `?v=<хеш>` кэшируются на год, список каталога не отдаётся.
- `GET /media/…` — варианты изображений, обработчик модуля `images` (`web.MediaPrefix`); кэш `immutable`, оригиналы
  не отдаются никогда. `GET /media` — 404.
- `GET /admin/login`, `POST /admin/login`, `POST /admin/logout`, `GET /admin` (дашборд); `/admin/` → 301 на `/admin`;
  прочие пути под `/admin/` — 404 админки, но только после входа (без сессии — редирект на вход).
- Раздел страниц: `GET /admin/pages` (список), `GET /admin/pages/new`, `POST /admin/pages` (создать),
  `GET|POST /admin/pages/{id}` (форма, сохранить), `GET|POST /admin/pages/{id}/delete` (подтверждение, удалить),
  `POST /admin/pages/preview` (htmx: превью Markdown).
- Медиатека: `GET /admin/media` (загрузка и сетка, `?page=N`), `POST /admin/media` (загрузка), `GET /admin/media/{id}`
  (карточка), `GET|POST /admin/media/{id}/delete` (подтверждение, удалить).
- Зарезервированные slug — `reservedSlugs` в `cmd/site/server.go` (`admin`, `static`, `media`, `healthz`): их маршруты перекрывают
  страницу, админка такие slug не принимает. Маршрут-поддерево (`/static/`) занимает и путь без слеша (`GET /static` → 404),
  иначе его забрал бы `GET /{slug}`; e2e `TestE2EReservedSlugsShadowPages` проверяет каждый slug из списка.
- Всё остальное — оформленная 404 (`web.NotFound`).
- `GET /healthz` — JSON о состоянии: `{"status":"ok","version":"<версия сборки>","schema":{"images":{"version":1,"pending":0},"site":{"version":4,"pending":0}}}`.

## Бинарник

`site serve | migrate | seed | user create EMAIL | reprocess-images -below N`. Миграции применяются только `site migrate`, никогда при старте.

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

## Изображения (модуль `images`)

Как подключать модуль — `modules/images/UPGRADE.md` (v0.1.0); здесь — как это сделано в эталоне.

- Сервис создаёт `openMedia(db, cfg)` в `cmd/site/main.go`: `images.NewDiskStorage(DATA_DIR/uploads)` +
  `images.NewService(db, st, images.Config{})` над БД сайта. Его получают `newMux`, `seed` и `reprocess-images`.
- Таблицы `images_*` сайт не читает и не пишет запросами — только через `images.Service`. Ссылка из контента
  на изображение — `TEXT REFERENCES images_files (id)` без `ON DELETE` (с этапа 8.5): тогда `Service.Delete`
  используемого изображения вернёт ошибку внешнего ключа (`database.IsForeignKeyViolation`), и медиатека отдаст 409.
- Вывод — `web.Picture(img, alt, sizes)` (`internal/web/picture.templ`): `<picture>` с `<source type="image/webp">`
  и `<img>` с JPEG-фолбэком (`src` — JPEG не шире 960 px), `srcset` по ширинам, `width`/`height` для пропорций,
  `loading="lazy"`. `sizes` — ширина картинки в вёрстке. Адрес варианта — `web.MediaURL(v)` (`/media/` + `v.Path`).
  Адреса вариантов меняются при повторной обработке, поэтому их не сохраняют в контенте — только id изображения.
- Медиатека — `internal/media/` (не тип контента: данные в модуле). Загрузка одного файла формой
  `multipart/form-data`, через htmx (`hx-encoding`, `hx-disabled-elt` на время обработки) и без JavaScript.
  Ошибки модуля (`images.ErrTooLarge`, `ErrTooManyPixels`, `ErrUnsupportedFormat`, `ErrInvalidImage`) — 422 с
  сообщением у поля `file`. Тело запроса ограничивает `maxBody(media.MaxUploadRequest, …)` снаружи `signedIn`:
  форма без JavaScript передаёт CSRF-токен полем multipart, и `RequireUser` разбирает тело раньше обработчика.
  Лимит вдвое больше `images.MaxBytes`, чтобы файл сверх 20 МБ дочитался и пользователь увидел ошибку, а не обрыв.
- Seed — 4 изображения из `cmd/site/seeddata/` (копии из `modules/images/testdata`, встроены через embed);
  уже загруженное с тем же именем файла пропускается.

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
  `admin.CSRFInput()` (скрытое поле токена), `admin.NotFound`, `admin.ServerError`, `admin.Redirect` (303 или
  `HX-Redirect` после успешного POST), `admin.IsHTMX`.
- htmx в админке: `<meta name="htmx-config">` разрешает подмену разметки ответом 422 (по умолчанию htmx не подменяет 4xx).
- Dev-пользователь из seed: `admin@example.com` / `admin` (короче политики — seed пишет напрямую и в проде не запускается).
- Не получается войти: проверить dev-пользователем (вход работает?); затем раскладку при `make user-create` и не остался ли
  запущенным сервер со старой БД после `make reset` (порт 8080 занят — `make dev` после seed падает с «address already in use»).
  После 5 неудач email заблокирован на 15 минут; перезапуск сервера снимает блокировку.

## Тип контента

Один тип — один пакет `internal/content/<type>/` (пример — `pages`):
`queries.sql` + код sqlc, `handlers.go` (`Handler`, `NewHandler(db)`, публичные хендлеры), `views.templ`,
`admin.go` (`Admin`, `NewAdmin(db, …)`, `AdminPath`, хендлеры раздела админки), `admin.templ`.
Миграция — в `db/migrations/`, маршруты — в `newMux`, dev-данные — в `cmd/site/seed.go`, запись sqlc — в `sqlc.yaml`,
счётчик дашборда — в `adminCounters`, пункт меню — в `admin.Options.Sections`.

Раздел админки (образец — `pages/admin.go`):
- Маршруты — `signedIn(...)`: сессия и CSRF уже проверены. Список, `new`, создание `POST /admin/<type>`,
  форма и сохранение `/{id}`, предпросмотр `/{id}/view`, удаление через страницу подтверждения `/{id}/delete`
  (работает без JavaScript).
- Кнопки формы: «Сохранить» и «Отмена» (ссылка на список, без сохранения). Удаление — не рядом с ними, а отдельным
  блоком `.danger-zone` под чертой; в строках списка ссылки «Удалить» нет.
- Feature `draft` (образец — `pages`): колонка `draft INTEGER NOT NULL DEFAULT 0 CHECK (draft IN (0, 1))` (STRICT не знает
  BOOLEAN; в Go — `bool` через `overrides` в `sqlc.yaml`), флажок «Черновик» в форме. Новая запись — черновик.
  Публичные запросы (страница, меню, позже sitemap и RSS) берут только `draft = 0`; в списке админки — колонка «Статус».
  У черновика в форме — «Предпросмотр» (`/{id}/view`: публичный шаблон с плашкой `.draft-banner`), у опубликованной —
  «Открыть на сайте».
- Удалить можно только черновик: опубликованную запись сначала снимают с публикации. Запрос удаления — с `AND draft = 1`;
  подтверждение и `POST` для опубликованной отдают 409 с объяснением. Главная страница (`home`) черновиком не бывает
  (ошибка у поля `draft`), поэтому не удаляется.
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
- `TestImagesContract` (`cmd/site/images_test.go`) — контрактные тесты модуля (`testkit.Run`) на интеграции сайта:
  `migratedSite` (миграции модуля и сайта, загрузки во временной папке) и маршруты `newMux`. При обновлении модуля
  они первыми покажут, что интеграция сломалась.
- Хелпер `migratedSite(t, withSeed)` (`main_test.go`) — БД после `migrate`, сервис изображений и `handler(cfg)`;
  `withSeed` заливает `seedContent` без изображений (их обработка ~1 с). Нужны картинки из seed — `seedImages(ctx, s.media)`.
- e2e медиатеки — `cmd/site/e2e_media_admin_test.go`: seed, загрузка (htmx и форма), каждая ошибка проверки файла,
  тело сверх лимита, карточка, удаление, 409 для используемого изображения, постраничный вывод.
- e2e раздела типа контента — `cmd/site/e2e_<type>_admin_test.go` (образец — `pages`): `newPageAdmin` (сайт + вошедший клиент),
  `save`, заголовки `htmx`; каждая ошибка валидации проверяется и без `HX-Request` (страница), и с ним (фрагмент).
- Браузерные тесты — `cmd/site/browser_test.go` (`//go:build browser`, `TestBrowser*`, chromedp, headless Chrome):
  только критичные htmx-взаимодействия — превью при наборе, сохранение через `HX-Redirect`, подмена формы с ошибкой
  без перезагрузки, загрузка файла (`hx-encoding="multipart/form-data"`). Запуск — `make e2e-browser`; `make test` их не собирает, линтер — собирает (`build-tags` в `.golangci.yml`).

## Спецификация

`site.yaml` — источник правды о структуре сайта: структурные изменения сначала вносятся в него, затем в код.
Проверка (из корня монорепо): `make spec-validate FILE=reference/site.yaml`.

## Структура

```
cmd/site/main.go            # точка входа: конфиг, подкоманды, источники миграций
cmd/site/server.go          # маршруты, данные шапки (siteLoader), /healthz
cmd/site/seed.go            # dev-данные: seedContent, seedImages
cmd/site/seeddata/          # изображения для seed (embed)
cmd/site/user.go            # site user create
cmd/site/e2e_test.go        # e2e (TestE2E*)
cmd/site/e2e_admin_test.go  # e2e админки
cmd/site/e2e_pages_admin_test.go  # e2e раздела «Страницы»
cmd/site/e2e_media_admin_test.go  # e2e медиатеки
cmd/site/images_test.go     # контрактные тесты модуля images (testkit)
cmd/site/browser_test.go    # браузерные тесты (тег browser, make e2e-browser)
db/migrations/              # миграции сайта (goose) + embed.go
internal/admin/             # админка: вход, сессии, CSRF, ограничение попыток, оболочка, дашборд
internal/content/pages/     # тип контента pages: queries.sql, sqlc, handlers.go, views.templ, admin.go, admin.templ
internal/database/          # открытие SQLite, статус схем, снимок, применение миграций
internal/media/             # медиатека в админке (модуль images)
internal/richtext/          # Markdown → очищенный HTML
internal/slug/              # slug: Make (транслитерация), Valid
internal/settings/          # site_settings: queries.sql и код sqlc
internal/web/               # layout, страницы ошибок, рендер, Picture
static/                     # CSS, htmx, иконка + static.go (embed, Path, Handler)
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: новые `internal/content/<type>/`.
