# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо и его `docs/`; этот файл и `docs/` описывают только сайт.

## Документация

Здесь — версии, команды, БД и миграции. Остальное — в `docs/`: эти файлы не загружаются сами, прочитай нужный
перед задачей.

| Задача | Файл |
|---|---|
| маршрут, новый пакет, где что лежит | `docs/structure.md` — маршруты, структура каталогов |
| тип контента: поля, черновик-копия, живой предпросмотр, блоки, поле-список, адрес раздела постов | `docs/content-types.md` |
| админка и вход, настройки сайта, меню | `docs/admin.md` |
| шаблоны, layout, данные шапки, `richtext`, статика, стили | `docs/templates.md` |
| изображения (модуль `images`), файлы (модуль `files`) | `docs/media.md` |
| SEO записи, sitemap, RSS, robots.txt, `<head>` | `docs/seo.md` |
| тесты: e2e, браузерные, контрактные, сверка `site.yaml` с кодом | `docs/tests.md` |
| переменные окружения, резервные копии, развёртывание, Docker, litestream | `docs/deploy.md` |

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.2`.
- Модули Keelkit (публичный репозиторий `github.com/keelkit/keelkit`, скачиваются как любые зависимости Go):
  `github.com/keelkit/keelkit/modules/images` v0.3.0, `github.com/keelkit/keelkit/modules/files` v0.2.0
  (таблицы `files_*`), `github.com/keelkit/keelkit/modules/richtext` v0.2.0 (без таблиц и миграций),
  `github.com/keelkit/keelkit/modules/auth` v0.3.0 (таблицы `users`, `sessions`);
  в `site.yaml` — `modules: { images: v0, files: v0, richtext: v0, auth: v0 }` (`spec` v0.8.0: `SPEC_VERSION` в `Makefile`).
  Их зависимости приходят транзитивно: `golang.org/x/image`, `github.com/gen2brain/webp` (+ `ebitengine/purego`);
  goldmark, bluemonday, `golang.org/x/net`; `golang.org/x/crypto` (argon2id).
- Тег сборки `nodynamic` (`BUILD_TAGS` в `Makefile`, экспортируется через `GOFLAGS`): кодек WebP не подгружает
  системную libwebp, а всегда работает на коде, транслированном в Go. Обработка изображений даёт одинаковый результат
  на любой машине, бинарник не зависит от библиотек хоста. Сборка и тесты без `make` — с `-tags nodynamic`.
- Зависимости: `modernc.org/sqlite` v1.60.1, `github.com/pressly/goose/v3` v3.28.0 (только библиотека, CLI goose не используется),
  `github.com/a-h/templ` v0.3.1020,
  `golang.org/x/term` v0.46.0 (ввод пароля без эха); в тестах — `github.com/PuerkitoBio/goquery` v1.13.0,
  `github.com/chromedp/chromedp` v0.16.0 (только браузерные тесты с тегом `browser`; его `cdproto` — псевдоверсия, теги у неё не ставятся).
- Инструменты (версии закреплены в `Makefile`): sqlc v1.31.1; CLI templ — `TEMPL_VERSION`, обязан совпадать с библиотекой
  в `go.mod` (`make generate` проверяет это и откажется запускаться при расхождении).
- Вендоренный JS: htmx 2.0.11 — `static/htmx-2.0.11.min.js` (сверен с sha512 из npm). Свой JS — `static/editor.js`
  (панель оформления Markdown в админке) и `static/live-preview.js` (живой предпросмотр в редакторе записи),
  без зависимостей и сборки.

## Команды

Все команды — через `make` (список: `make help`).

| Цель | Что делает |
|---|---|
| `make build` | статический бинарник `bin/site` (`CGO_ENABLED=0`, `-trimpath`); версия — тег сайта `vX.Y.Z` или хеш коммита (`-dirty` при незакоммиченных правках), видна в `/healthz` и логе старта. Для VPS: `make build GOOS=linux GOARCH=amd64`; `VERSION=…` задаёт версию явно |
| `make run` | запустить сайт (`ADDR`, `DATA_DIR`, `BASE_URL`); при непримененных миграциях отказывается стартовать |
| `make dev` | для разработки: `migrate`, `seed`, запуск |
| `make migrate` | снимок БД в `BACKUP_DIR` (`VACUUM INTO`), затем миграции: модули в порядке `site.yaml`, потом сайт |
| `make docker` | образ Docker (опция) из бинарника `make build` под Linux, см. «Развёртывание» в `docs/deploy.md`; `DOCKER_ARCH=amd64` — для VPS на amd64 |
| `make backup` | снимок БД и архив загрузок в `BACKUP_DIR` (`site backup`, см. «Резервные копии» в `docs/deploy.md`); сервер может работать |
| `make seed` | dev-данные; идемпотентно, в проде не запускается; печатает dev-пользователя админки |
| `make reprocess-images BELOW=N` | повторная обработка изображений с версией профиля ниже `N` текущим профилем; идемпотентна и безопасна для прерывания (повторный запуск доделывает), сервер может работать. Печатает `processed`/`failed` |
| `make user-create EMAIL=…` | пользователь админки; пароль спрашивается дважды без эха (не из терминала — строками из stdin). Ввод не виден — проверьте раскладку: пароль, дважды набранный кириллицей, совпадёт сам с собой, но не с латиницей в браузере. |
| `make user-password EMAIL=…` | сменить пароль пользователя админки (забыли пароль, утёк); ввод как у `user-create`; все сессии пользователя завершаются |
| `make reset` | удалить `DATA_DIR` (БД и загрузки); снимки в `backups/` остаются. Сначала остановите сервер: запущенный процесс продолжит работать с удалённой БД, а новый `make dev` не займёт порт |
| `make generate` | кодогенерация: templ, затем sqlc |
| `make schema` | записать итоговую схему БД в `db/schema.sql` (после новой миграции) |
| `make generate-check` | проверить, что генерация ничего не меняет (входит в `make check` монорепо) |
| `make test` | все тесты, включая e2e |
| `make e2e` | только e2e (`TestE2E*` в `cmd/site`) |
| `make e2e-browser` | браузерные тесты админки: htmx и свой JS (`TestBrowser*`, тег `browser`, chromedp; нужен Chrome) |
| `make test-isolated` | тесты с `GOWORK=off`, только на опубликованных версиях модулей |
| `make lint` | golangci-lint, версия закреплена в `Makefile` |

## Бинарник

`site serve | migrate | backup | seed | user create EMAIL | user password EMAIL | reprocess-images -below N`. Миграции применяются только `site migrate`, никогда при старте.

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
  Тип с черновиком-копией (образец — `pages`) делится на две таблицы — см. «Черновик-копия» в `docs/content-types.md`.
- Необязательные поля `image` и `ref` — `NULL` (в Go `sql.NullString`/`sql.NullInt64`): у пустой строки или нуля нет
  строки, на которую ссылается внешний ключ. Ссылка без `ON DELETE`: используемое изображение или категорию не удалить
  (образец — `post_contents.cover`, `post_contents.category_id`).
- Поле `image` с `mobile: true` в `site.yaml` (с `spec v0.7.0`; образец — обложка поста) — ещё колонка
  `<поле>_mobile` той же формы (`post_contents.cover_mobile`, миграция `00015`): своя картинка для телефона.
- Даты (`publish_date`) — `TEXT` в UTC, формат `2006-01-02T15:04:05Z`: такие строки сравниваются в SQL как время
  (`publish_date <= ?` с текущим временем параметром).
- Миграция, меняющая живые данные (образец — `00005_page_contents.sql`): старая таблица переименовывается
  в `<type>_legacy` и удаляется только следующей миграцией (двухэтапно); тест на копии реалистичных данных —
  `cmd/site/migrations_test.go` (`migrateTo` доводит БД до версии перед миграцией, затем штатный `migrate`).
  `pages_legacy` ещё не удалена (`00006`, `00007` её не трогают): удалит отдельная миграция `DROP TABLE pages_legacy`.
- `db/schema.sql` — итоговая схема после всех миграций (модули и сайт), чтобы не собирать таблицу по цепочке
  миграций; не редактируется руками: `make schema` после новой миграции, `TestSchemaSnapshot` (`cmd/site/schema_test.go`)
  падает, если файл отстал. В нём видна и история (`pages_legacy`) — источник правды всё равно миграции.
- **`queries.sql` — только ASCII**, включая комментарии: sqlc v1.31.1 для SQLite вырезает текст запроса по смещениям
  в байтах, и кириллица в файле обрезает запросы. Русские тексты передаются параметрами. В миграциях кириллица допустима.

## Спецификация

`site.yaml` — источник правды о структуре сайта: структурные изменения сначала вносятся в него, затем в код.
Проверка (из корня монорепо): `make spec-validate FILE=reference/site.yaml`.
