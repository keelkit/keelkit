# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо; этот файл описывает только сайт.

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.2`.
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
Для gopls в IDE (запускается вне `make`) — один раз `go env -w 'GOPRIVATE=github.com/voffkin/*'`. В CI — deploy key только для чтения (CI до этапа 11 запускается вручную, проверки — локально `make check` из корня монорепо).

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
| `make generate-check` | проверить, что генерация ничего не меняет (входит в `make check` монорепо) |
| `make test` | все тесты, включая e2e |
| `make e2e` | только e2e (`TestE2E*` в `cmd/site`) |
| `make e2e-browser` | браузерные тесты htmx-взаимодействий (`TestBrowser*`, тег `browser`, chromedp; нужен Chrome) |
| `make test-isolated` | тесты с `GOWORK=off`, только на опубликованных версиях модулей |
| `make lint` | golangci-lint, версия закреплена в `Makefile` |

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8080` (в `make run`/`dev` — `:9090`) | адрес HTTP-сервера |
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками (`uploads/`: оригиналы и варианты изображений) |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями |
| `COOKIE_SECURE` | `true` | флаг `Secure` у cookie админки; `false` — только для разработки по http (`make run`/`make dev` передают `false`) |
| `TZ` | часовой пояс системы | в нём показываются и вводятся даты публикации постов (в БД — UTC) |

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — опубликованная копия страницы из `pages` (`page_contents`, `kind = 'published'`); `/home` → 301 на `/`.
- Блог: `GET /blog` (лента, `?page=N`, по 10 постов), `GET /blog/{slug}` (пост), `GET /blog/category/{slug}` (лента
  категории, `?page=N`). Только опубликованные копии с наступившей датой публикации; несуществующая или пустая
  страница ленты (кроме первой), неверный `?page` — 404. В меню сайта блог — последним пунктом после страниц.
- `GET /static/…` — файлы из `static/` (embed); ссылки с `?v=<хеш>` кэшируются на год, список каталога не отдаётся.
- `GET /media/…` — варианты изображений, обработчик модуля `images` (`web.MediaPrefix`); кэш `immutable`, оригиналы
  не отдаются никогда. `GET /media` — 404.
- `GET /admin/login`, `POST /admin/login`, `POST /admin/logout`, `GET /admin` (дашборд); `/admin/` → 301 на `/admin`;
  прочие пути под `/admin/` — 404 админки, но только после входа (без сессии — редирект на вход).
- Раздел страниц: `GET /admin/pages` (список), `GET /admin/pages/new`, `POST /admin/pages` (создать),
  `GET|POST /admin/pages/{id}` (форма; «Сохранить», «Опубликовать» или кнопка блока — поле `action`),
  `GET /admin/pages/{id}/view` (предпросмотр черновика), `POST /admin/pages/{id}/unpublish` (снять с публикации),
  `GET|POST /admin/pages/{id}/delete` (подтверждение, удалить), `POST /admin/pages/preview?field=<поле>` (htmx: превью Markdown),
  `POST /admin/pages/block?type=<тип>|?id=<id>` (htmx: фрагмент формы нового блока или блока из присланной формы).
- Раздел постов `/admin/posts` — те же маршруты, что у страниц, и `POST /admin/posts/cover` (htmx: поле обложки с превью).
- Раздел категорий: `GET /admin/categories` (список), `GET /admin/categories/new`, `POST /admin/categories`,
  `GET|POST /admin/categories/{id}` (форма), `GET|POST /admin/categories/{id}/delete` (подтверждение, удалить).
- Медиатека: `GET /admin/media` (загрузка и сетка, `?page=N`), `POST /admin/media` (загрузка), `GET /admin/media/{id}`
  (карточка), `GET|POST /admin/media/{id}/delete` (подтверждение, удалить).
- Зарезервированные slug — `reservedSlugs` в `cmd/site/server.go` (`admin`, `static`, `media`, `healthz`, `blog`): их маршруты перекрывают
  страницу, админка такие slug не принимает. Маршрут-поддерево (`/static/`) занимает и путь без слеша (`GET /static` → 404),
  иначе его забрал бы `GET /{slug}`; e2e `TestE2EReservedSlugsShadowPages` проверяет каждый slug из списка.
- Всё остальное — оформленная 404 (`web.NotFound`).
- `GET /healthz` — JSON о состоянии: `{"status":"ok","version":"<версия сборки>","schema":{"images":{"version":1,"pending":0},"site":{"version":6,"pending":0}}}`.

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
  Тип с черновиком-копией (образец — `pages`) делится на две таблицы — см. «Черновик-копия».
- Необязательные поля `image` и `ref` — `NULL` (в Go `sql.NullString`/`sql.NullInt64`): у пустой строки или нуля нет
  строки, на которую ссылается внешний ключ. Ссылка без `ON DELETE`: используемое изображение или категорию не удалить
  (образец — `post_contents.cover`, `post_contents.category_id`).
- Даты (`publish_date`) — `TEXT` в UTC, формат `2006-01-02T15:04:05Z`: такие строки сравниваются в SQL как время
  (`publish_date <= ?` с текущим временем параметром).
- Миграция, меняющая живые данные (образец — `00005_page_contents.sql`): старая таблица переименовывается
  в `<type>_legacy` и удаляется только следующей миграцией (двухэтапно); тест на копии реалистичных данных —
  `cmd/site/migrations_test.go` (`migrateTo` доводит БД до версии перед миграцией, затем штатный `migrate`).
  `pages_legacy` ещё не удалена (`00006` её не трогает): удалит отдельная миграция `DROP TABLE pages_legacy`.
- **`queries.sql` — только ASCII**, включая комментарии: sqlc v1.31.1 для SQLite вырезает текст запроса по смещениям
  в байтах, и кириллица в файле обрезает запросы. Русские тексты передаются параметрами. В миграциях кириллица допустима.

## Изображения (модуль `images`)

Как подключать модуль — `modules/images/UPGRADE.md` (v0.1.0); здесь — как это сделано в эталоне.

- Сервис создаёт `openMedia(db, cfg)` в `cmd/site/main.go`: `images.NewDiskStorage(DATA_DIR/uploads)` +
  `images.NewService(db, st, images.Config{})` над БД сайта. Его получают `newMux`, `seed` и `reprocess-images`.
- Таблицы `images_*` сайт не читает и не пишет запросами — только через `images.Service`. Ссылка из контента
  на изображение — `TEXT REFERENCES images_files (id)` без `ON DELETE` (`page_images`): тогда `Service.Delete`
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
  уже загруженное с тем же именем файла пропускается. `seedImages` возвращает id по имени файла, `seedContent`
  вставляет их в блоки «О проекте» (без изображений — тесты — эти блоки пропускаются).
- Картинки в контенте — только блоками (`image`, `image_text`) с id изображения и полями `image` (обложка поста);
  ссылки — в `page_images` (`<type>_images` для других типов, у постов — и обложка), перезаписываются в той же
  транзакции, что и копия. Медиатека не знает о типах
  контента: «где используется» ей передаёт `imageUsage` в `cmd/site/server.go` (`media.UsageFunc`) — новый тип
  со ссылками на изображения добавляет туда свой запрос (страницы, затем посты). Используемое изображение: на карточке — список страниц и постов,
  подтверждение удаления и `POST` — 409 со списком.

## Шаблоны и HTML

- Шаблоны — templ (`*.templ` рядом с кодом, `*_templ.go` генерируются и коммитятся).
- `internal/web`: `Layout(title)` — оболочка страницы, `LayoutMeta(title, description)` — с описанием страницы (пусто — описание сайта); `NotFoundPage`, `ErrorPage`; `Render` (рендер в буфер, затем ответ),
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
  запущенным сервер со старой БД после `make reset` (порт 9090 занят — `make dev` после seed падает с «address already in use»).
  После 5 неудач email заблокирован на 15 минут; перезапуск сервера снимает блокировку.

## Тип контента

Один тип — один пакет `internal/content/<type>/` (образцы — `pages`; `posts` — то же с обложкой, категорией и датой
публикации; `categories` — справочник без черновика):
`queries.sql` + код sqlc, `handlers.go` (`Handler`, `NewHandler(db, images)`, публичные хендлеры), `views.templ`,
`admin.go` (`Admin`, `NewAdmin(db, media, mediaURL, reserved)`, `AdminPath`, `EditPath`, хендлеры раздела админки,
`SeedPublished` для seed), `admin.templ`. Поле `blocks` — через `internal/blocks/` (см. «Блоки»).
Миграция — в `db/migrations/`, маршруты — в `newMux`, dev-данные — в `cmd/site/seed.go`, запись sqlc — в `sqlc.yaml`,
счётчик дашборда — в `adminCounters`, пункт меню — в `admin.Options.Sections`.
Seed блога (`seedBlog`): 3 категории, 13 опубликованных постов (`post-01`…`post-13`, даты — дни назад от запуска,
обложки у каждого третьего), черновик `draft-post` и `future-post` с датой через 30 дней.

Раздел админки (образец — `pages/admin.go`):
- Маршруты — `signedIn(...)`: сессия и CSRF уже проверены. Список, `new`, создание `POST /admin/<type>`,
  форма и сохранение `/{id}`, предпросмотр черновика `/{id}/view`, снятие с публикации `POST /{id}/unpublish`,
  удаление через страницу подтверждения `/{id}/delete` (работает без JavaScript).
- Кнопки формы (черновик-копия — см. ниже): «Сохранить» (черновик), «Опубликовать», «Отмена» (ссылка на список, без
  сохранения). «Снять с публикации» и удаление — не рядом с ними, а отдельным блоком `.danger-zone` под чертой;
  в строках списка ссылки «Удалить» нет. Удалить можно только неопубликованную запись (запрос — `AND NOT EXISTS` опубликованной копии);
  подтверждение и `POST` для опубликованной отдают 409 с объяснением. Главная (`home`) всегда опубликована:
  не снимается с публикации (409), не удаляется, опубликованная главная не меняет slug (ошибка у поля).
- Форма отправляется и обычным POST, и через htmx (`hx-post`, `hx-target="this"`, `hx-swap="outerHTML"`).
  Ошибки валидации — 422: при `HX-Request` только фрагмент формы, без него — страница целиком; введённые данные
  возвращаются в форму, сообщение — у поля (`#<поле>-error`, `aria-invalid`, `aria-describedby`).
- Успех: обычной форме — 303 на форму с `?saved=1` или `?published=1`, htmx — 204 и `HX-Redirect`.
- Нормализация в `validate`: `TrimSpace` заголовка, CRLF → LF в тексте. Уникальность — ограничением `UNIQUE` в БД
  (`database.IsUniqueViolation`), без предварительного `SELECT`: так не проходит и гонка двух запросов.
- slug — `internal/slug`: пустой составляется из заголовка (`slug.Make`, транслитерация кириллицы своей таблицей),
  введённый вручную приводится к тому же виду; затем проверка `reservedSlugs` и уникальности.
- Поле `image` (обложка поста): `select` из медиатеки (последние `libraryLimit` и выбранное) и превью; смена выбора
  через htmx перерисовывает только поле (`POST …/cover`, `hx-params` — только это поле). Выбранного изображения нет
  в медиатеке — ошибка у поля.
- Поле `ref` (категория поста): `select` с вариантом «— без категории —»; варианты — запрос в пакете типа
  (`ListCategoryOptions`), а не импорт пакета другого типа. Несуществующий id — ошибка у поля, не число — 400.
- Справочник без черновика (образец — `categories`): форма «Сохранить» и «Отмена», правки сразу на сайте;
  удаление через подтверждение, используемую запись — 409 со списком (запрос и ошибка внешнего ключа).
  Публичную страницу справочника отдаёт тип, который на него ссылается (лента категории — пакет `posts`),
  поэтому адреса для админки категорий передаёт `newMux` (`categories.NewAdmin(db, posts.CategoryPath, posts.EditPath)`).
- Поле `richtext` (и Markdown в блоках): textarea + превью (`hx-post` на `…/preview?field=<имя>`, `hx-params` — только это поле,
  `hx-trigger="input changed delay:400ms"`, `hx-sync="this:replace"`); превью при открытии формы рисует сервер,
  ответ превью — тот же `richtext.Render`.

### Черновик-копия (образец — `pages`)

Заменяет флажок `draft` (решение в корневом `AGENTS.md`): правки опубликованной записи не видны на сайте до «Опубликовать».
- Таблица `<type>s` — только идентичность (`id`, `created_at`, `updated_at`); содержимое — `<type>_contents(<type>_id,
  kind IN ('draft','published'), поля…)`, `PRIMARY KEY (<type>_id, kind)`. Черновик есть всегда, опубликованная копия —
  если запись на сайте. slug — часть копии: уникален частичными индексами `WHERE kind = 'draft'` и `WHERE kind = 'published'`,
  адрес на сайте меняется при публикации.
- «Сохранить» (`action=save`, и Enter в поле — первая кнопка формы скрытая «Сохранить») пишет черновик и его ссылки на
  изображения одной транзакцией; «Опубликовать» (`publish`) — то же и копирует черновик в опубликованную (`INSERT … SELECT …
  ON CONFLICT DO UPDATE`) вместе со ссылками; «Снять с публикации» — `DELETE` опубликованной копии (ссылки — каскадом).
- Публичные запросы (страница, меню, позже sitemap и RSS) берут только `kind = 'published'`; админка — черновик.
- Статус в списке и в форме: «Черновик» (нет опубликованной), «Опубликована», «Есть неопубликованные изменения»
  (черновик отличается от опубликованной: сравниваются колонки, блоки — строкой JSON, `blocks.Marshal` детерминирован).
- Предпросмотр `/{id}/view` — черновик в публичном шаблоне с плашкой `.draft-banner`; «Открыть на сайте» — по
  опубликованному slug.
- Feature `publish_date` (образец — `posts`): дата — часть копии. Поле `datetime-local` в часовом поясе сервера;
  пустое — «в момент публикации»: «Опубликовать» ставит текущее время (до минуты, как в поле) в черновик и копию.
  Опубликованная копия с датой в будущем на сайте не видна до этой даты; статус — «Запланирован на …», ссылки
  «Открыть на сайте» нет. Лента — по дате публикации, новые сверху.

### Блоки (`internal/blocks/`)

Поле `{ type: blocks, allow: [...] }` из `site.yaml` — JSON-массив в колонке (`CHECK (json_valid(…) AND json_type(…) = 'array')`).
Общие поля блока: `id` (стабильный: якорь `block-<id>` на сайте, имена полей формы `block-<id>-<поле>`), `type`, `hidden`
(остаётся в данных и в админке, на сайт не выводится). Типы: `text` (Markdown), `image` (изображение, alt обязателен,
подпись), `image_text` (изображение, alt, Markdown, `position` left/right), `quote` (цитата, автор).
- Чтение (`blocks.Unmarshal`): нет поля — нулевое значение (смысл по умолчанию задаёт тип); нет или повторный id — новый;
  неизвестный тип — `*blocks.Unknown`: на сайт не выводится (предупреждение в лог), в админке сохраняется как есть.
  Ломающее изменение полей блока — миграция данных по строгим правилам.
- Форма: блоки по порядку (`block` — id, затем `block-<id>-type`, `block-<id>-hidden`, поля типа). Кнопки блока —
  submit с `name="action"`: `up:<id>`, `down:<id>`, `hide:<id>`, `remove:<id>`, `add:<тип>` (`blocks.Apply`); сервер
  возвращает форму изменённой (200, плашка «Изменения ещё не сохранены»), ничего не сохраняя. Без JavaScript — обычный
  POST всей формы, с htmx — подмена формы. «Добавить блок» через htmx запрашивает только фрагмент блока
  (`hx-post …/block?type=`, `hx-params="none"`, `hx-swap="beforeend"`); смена изображения перерисовывает блок (`?id=`).
- Проверка — `blocks.Validate` (все блоки, и скрытые); ошибки — у поля блока (`#block-<id>-<поле>-error`), 422 как у
  обычных полей. Изображения загружает `blocks.Resolve` (интерфейс `blocks.Images` — `Get` модуля).
- `blocks.ImageIDs` — для `<type>_images` (скрытые блоки тоже держат изображение), `blocks.PlainText` и `blocks.Summary` —
  текст для описания страницы (`web.LayoutMeta`), позже RSS и поиска.
- Список блоков поля — в коде типа (`pages.BodyBlocks`, `posts.BodyBlocks`); `TestSpecBlocksMatchCode` сверяет каждое
  поле `blocks` из `site.yaml` со списком своего типа (новое поле `blocks` — запись в карте `code` теста).

Как добавить тип блока: запись в `blocks.Kinds`; структура с JSON-тегами и методами `Content` в `types.go`
(`readForm`, `check` — нормализация и ошибки, `view`, `form`, `imageIDs`, `plainText`); templ-вывод в `view.templ`
(корень — элемент с классом `block block-<тип>` и `id="block-<id>"`) и фрагмент формы в `form.templ`; стили; тест в
`blocks_test.go`; имя — в `allow` нужного поля `site.yaml` и в списке блоков типа контента.

## Тесты

- Unit-тесты — рядом с пакетом (`internal/richtext`, `internal/database`, `internal/admin`, `internal/slug`, `internal/blocks`).
- e2e — `cmd/site/e2e_test.go`, функции `TestE2E*`: `httptest.NewServer(newMux(db, config{}))` над БД после `migrate` и `seed`,
  HTML разбирается goquery. Хелперы `fetch` (тело уже прочитано и закрыто) и `page` (статус, `Content-Type`, документ).
- e2e админки — `cmd/site/e2e_admin_test.go`: `adminClient` (cookie jar, редиректы не выполняются), `post`, `login`, `loginOK`
  (вход dev-пользователем, возвращает CSRF-токен сессии), `csrfToken` (скрытое поле формы).
- `TestImagesContract` (`cmd/site/images_test.go`) — контрактные тесты модуля (`testkit.Run`) на интеграции сайта:
  `migratedSite` (миграции модуля и сайта, загрузки во временной папке) и маршруты `newMux`. При обновлении модуля
  они первыми покажут, что интеграция сломалась.
- Хелпер `migratedSite(t, withSeed)` (`main_test.go`) — БД после `migrate`, сервис изображений и `handler(cfg)`;
  `withSeed` заливает `seedContent` без изображений (их обработка ~1 с). Нужны картинки из seed — `migratedSiteWithImages(t)`
  (полный seed, возвращает id изображений по имени файла).
- e2e медиатеки — `cmd/site/e2e_media_admin_test.go`: seed, загрузка (htmx и форма), каждая ошибка проверки файла,
  тело сверх лимита, карточка, удаление, 409 для используемого изображения, постраничный вывод.
- e2e раздела типа контента — `cmd/site/e2e_<type>_admin_test.go` (образец — `pages`): `newPageAdmin` (сайт + вошедший клиент),
  `form` (поля формы так, как их отправит браузер — `formValues`; другая форма — `formByID`), `submit(path, form, action, header)`, `addBlock`,
  заголовки `htmx`; каждая ошибка валидации проверяется и без `HX-Request` (страница), и с ним (фрагмент).
- e2e блога — `cmd/site/e2e_blog_test.go` (лента, страницы, категория, пост, скрытые черновик и будущий пост) и
  `cmd/site/e2e_posts_admin_test.go` (посты и категории в админке; хелперы `createPost`, `postForm`, `newPostForm`).
- `TestSpecBlocksMatchCode` (`cmd/site/spec_test.go`) — блоки из `allow` в `site.yaml` есть в коде и совпадают со списком типа.
- Браузерные тесты — `cmd/site/browser_test.go` (`//go:build browser`, `TestBrowser*`, chromedp, headless Chrome):
  только критичные htmx-взаимодействия — превью при наборе, сохранение через `HX-Redirect`, подмена формы с ошибкой
  без перезагрузки, «Добавить блок», «↑», «Скрыть» и выбор изображения у блока, выбор обложки поста, загрузка файла (`hx-encoding="multipart/form-data"`).
  После каждого htmx-действия — `htmxSettled()`: обработчики на новый фрагмент htmx навешивает после фазы settle,
  и клик chromedp раньше этого ушёл бы обычной отправкой формы. Запуск — `make e2e-browser`; `make test` их не собирает, линтер — собирает (`build-tags` в `.golangci.yml`).

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
cmd/site/e2e_blog_test.go         # e2e блога на сайте
cmd/site/e2e_posts_admin_test.go  # e2e разделов «Посты» и «Категории»
cmd/site/migrations_test.go # миграции, меняющие данные, на копии реалистичных данных
cmd/site/spec_test.go       # site.yaml ↔ код (блоки)
cmd/site/images_test.go     # контрактные тесты модуля images (testkit)
cmd/site/browser_test.go    # браузерные тесты (тег browser, make e2e-browser)
db/migrations/              # миграции сайта (goose) + embed.go
internal/admin/             # админка: вход, сессии, CSRF, ограничение попыток, оболочка, дашборд
internal/blocks/            # поля blocks: типы блоков, JSON, форма, вывод на сайте
internal/content/pages/     # тип контента pages: queries.sql, sqlc, handlers.go, views.templ, admin.go, admin.templ
internal/content/posts/     # посты: то же + dates.go (дата публикации); лента категории — тоже здесь
internal/content/categories/ # категории: queries.sql, sqlc, admin.go, admin.templ (без публичных шаблонов)
internal/database/          # открытие SQLite, статус схем, снимок, применение миграций
internal/media/             # медиатека в админке (модуль images)
internal/richtext/          # Markdown → очищенный HTML
internal/slug/              # slug: Make (транслитерация), Valid
internal/settings/          # site_settings: queries.sql и код sqlc
internal/web/               # layout (Layout, LayoutMeta), страницы ошибок, рендер, Picture
static/                     # CSS, htmx, иконка + static.go (embed, Path, Handler)
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: новые `internal/content/<type>/`.
