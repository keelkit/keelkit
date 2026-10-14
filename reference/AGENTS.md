# AGENTS.md — эталонный сайт Keelkit

Эталонный сайт: образец устройства любого сайта Keelkit. Новые сайты повторяют его структуру и конвенции.
Общие правила проекта — в корневом `AGENTS.md` монорепо; этот файл описывает только сайт.

## Версии

- Go: `go 1.26.0` в `go.mod` (актуальный релиз минус один минор), тулчейн `go1.27.2`.
- Модули Keelkit: `github.com/voffkin/keelkit/modules/images` v0.1.0, `github.com/voffkin/keelkit/modules/files` v0.1.0
  (таблицы `files_*`), `github.com/voffkin/keelkit/modules/richtext` v0.1.1 (без таблиц и миграций),
  `github.com/voffkin/keelkit/modules/auth` v0.1.0 (таблицы `users`, `sessions`);
  в `site.yaml` — `modules: { images: v0, files: v0, richtext: v0, auth: v0 }` (`spec` v0.7.0: `SPEC_VERSION` в `Makefile`).
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

## Доступ к модулям Keelkit

Модули Keelkit лежат в приватном репозитории `github.com/voffkin/keelkit`. `Makefile` экспортирует `GOPRIVATE`
и правило git `insteadOf` (через `GIT_CONFIG_*`), поэтому `make` скачивает модули по SSH, не трогая глобальные
настройки git и Go. Нужен только SSH-ключ с доступом к репозиторию: `ssh -T git@github.com` должен поздороваться.
Для gopls в IDE (запускается вне `make`) — один раз `go env -w 'GOPRIVATE=github.com/voffkin/*'`. В CI — deploy key только для чтения (CI до этапа 14 запускается вручную, проверки — локально `make check` из корня монорепо).

## Команды

Все команды — через `make` (список: `make help`).

| Цель | Что делает |
|---|---|
| `make build` | статический бинарник `bin/site` (`CGO_ENABLED=0`, `-trimpath`); версия — тег сайта `vX.Y.Z` или хеш коммита (`-dirty` при незакоммиченных правках), видна в `/healthz` и логе старта. Для VPS: `make build GOOS=linux GOARCH=amd64`; `VERSION=…` задаёт версию явно |
| `make run` | запустить сайт (`ADDR`, `DATA_DIR`, `BASE_URL`); при непримененных миграциях отказывается стартовать |
| `make dev` | для разработки: `migrate`, `seed`, запуск |
| `make migrate` | снимок БД в `BACKUP_DIR` (`VACUUM INTO`), затем миграции: модули в порядке `site.yaml`, потом сайт |
| `make docker` | образ Docker (опция) из бинарника `make build` под Linux, см. «Развёртывание»; `DOCKER_ARCH=amd64` — для VPS на amd64 |
| `make backup` | снимок БД и архив загрузок в `BACKUP_DIR` (`site backup`, см. «Резервные копии»); сервер может работать |
| `make seed` | dev-данные; идемпотентно, в проде не запускается; печатает dev-пользователя админки |
| `make reprocess-images BELOW=N` | повторная обработка изображений с версией профиля ниже `N` текущим профилем; идемпотентна и безопасна для прерывания (повторный запуск доделывает), сервер может работать. Печатает `processed`/`failed` |
| `make user-create EMAIL=…` | пользователь админки; пароль спрашивается дважды без эха (не из терминала — строками из stdin). Ввод не виден — проверьте раскладку: пароль, дважды набранный кириллицей, совпадёт сам с собой, но не с латиницей в браузере. Смены пароля пока нет |
| `make reset` | удалить `DATA_DIR` (БД и загрузки); снимки в `backups/` остаются. Сначала остановите сервер: запущенный процесс продолжит работать с удалённой БД, а новый `make dev` не займёт порт |
| `make generate` | кодогенерация: templ, затем sqlc |
| `make schema` | записать итоговую схему БД в `db/schema.sql` (после новой миграции) |
| `make generate-check` | проверить, что генерация ничего не меняет (входит в `make check` монорепо) |
| `make test` | все тесты, включая e2e |
| `make e2e` | только e2e (`TestE2E*` в `cmd/site`) |
| `make e2e-browser` | браузерные тесты админки: htmx и свой JS (`TestBrowser*`, тег `browser`, chromedp; нужен Chrome) |
| `make test-isolated` | тесты с `GOWORK=off`, только на опубликованных версиях модулей |
| `make lint` | golangci-lint, версия закреплена в `Makefile` |

## Конфигурация (переменные окружения)

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `ADDR` | `:8080` (в `make run`/`dev` — `:9090`) | адрес HTTP-сервера |
| `DATA_DIR` | `data` | каталог с БД (`site.db`) и загрузками (`uploads/`: оригиналы и варианты изображений; `files/`: файлы модуля `files`) |
| `BACKUP_DIR` | `backups` | каталог снимков БД перед миграциями и резервных копий `site backup` |
| `COOKIE_SECURE` | `true` | флаг `Secure` у cookie админки и согласия с баннером cookies; `false` — только для разработки по http (`make run`/`make dev` передают `false`) |
| `TZ` | часовой пояс системы | в нём показываются и вводятся даты публикации постов (в БД — UTC) |
| `BASE_URL` | — (в `make run`/`dev` — `http://localhost:<порт из ADDR>`) | адрес сайта (`https://example.com`, без пути) для canonical, Open Graph, sitemap, RSS и robots.txt. **Обязателен для `serve`**: без него сайт не стартует (адрес из заголовка `Host` подделывается). Слеш в конце отбрасывается |

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — опубликованная копия страницы из `pages` (`page_contents`, `kind = 'published'`); `/home` → 301 на `/`.
  Прежний slug страницы (`page_redirects`) — 301 на её текущий адрес; у постов так же (`post_redirects`), если пост виден.
- Блог — под `posts.BlogPath` (в эталоне `/blog`, см. «Адрес раздела постов»): `GET /blog` (лента, `?page=N`, по 10
  постов), `GET /blog/{slug...}` (пост; иной путь — старый адрес поста, 301), `GET /blog/category/{slug}` (лента
  категории, `?page=N`). Только опубликованные копии с наступившей датой публикации; несуществующая или пустая
  страница ленты (кроме первой), неверный `?page` — 404. В меню блог попадает пунктом-разделом (см. «Меню»).
- `POST /cookie-notice` — согласие с баннером cookies (`web.AcceptCookies`, см. «Настройки сайта»); `GET /cookie-notice` —
  обычный `GET /{slug}`, поэтому в `reservedSlugs` его нет.
- `GET /sitemap.xml`, `GET /rss.xml`, `GET /robots.txt` — пакет `internal/seo` (см. «SEO»); без данных шапки.
- `GET /static/…` — файлы из `static/` (embed); ссылки с `?v=<хеш>` кэшируются на год, список каталога не отдаётся.
- `GET /files/<id>/<имя>` — файлы (PDF, видео, SVG), обработчик модуля `files` (`web.FilesPrefix`): тип по формату,
  Range, `ETag` версии и кэш с проверкой (адрес не меняется при замене), `nosniff`, у SVG — CSP `sandbox`.
  Другое имя при верном id и `GET /files` — 404.
- `GET /media/…` — варианты изображений, обработчик модуля `images` (`web.MediaPrefix`); кэш `immutable`, оригиналы
  не отдаются никогда. `GET /media` — 404.
- `GET /admin/login`, `POST /admin/login`, `POST /admin/logout`, `GET /admin` (дашборд); `/admin/` → 301 на `/admin`;
  прочие пути под `/admin/` — 404 админки, но только после входа (без сессии — редирект на вход).
- Раздел страниц: `GET /admin/pages` (список), `GET /admin/pages/new`, `POST /admin/pages` (создать),
  `GET|POST /admin/pages/{id}` (форма; «Сохранить», «Опубликовать» или кнопка блока — поле `action`),
  `GET /admin/pages/{id}/view` (предпросмотр черновика), `POST /admin/pages/{id}/unpublish` (снять с публикации),
  `GET|POST /admin/pages/{id}/delete` (подтверждение, удалить), `POST /admin/pages/preview?field=<поле>` (htmx: превью Markdown),
  `POST /admin/pages/block?type=<тип>|?id=<id>` (htmx: фрагмент формы нового блока или блока из присланной формы),
  `POST /admin/pages/seo` (htmx: превью сниппета SEO из присланной формы),
  `POST /admin/pages/live` (живой предпросмотр: страница сайта из присланной формы, ничего не сохраняет; без id —
  и для новой страницы).
- Раздел постов `/admin/posts` — те же маршруты, что у страниц, и `POST /admin/posts/cover` (htmx: поле обложки с превью).
- Раздел категорий: `GET /admin/categories` (список), `GET /admin/categories/new`, `POST /admin/categories`,
  `GET|POST /admin/categories/{id}` (форма), `GET|POST /admin/categories/{id}/delete` (подтверждение, удалить).
- Раздел людей: `GET /admin/people` (список в порядке на сайте), `POST /admin/people/order` (кнопка ↑ ↓: поле `action` —
  `up:<id>` или `down:<id>`; без htmx — 303 на список, с htmx — таблица), `GET /admin/people/new`, `POST /admin/people`,
  `GET|POST /admin/people/{id}` (форма), `POST /admin/people/photo` (htmx: поле фото с превью),
  `GET|POST /admin/people/{id}/delete` (подтверждение, удалить). Своих адресов на сайте у людей нет.
- Настройки: `GET|POST /admin/settings` (форма; «Сохранить» или кнопка списка соцсетей — поле `action`),
  `POST /admin/settings/image` (htmx: поле картинки для соцсетей с превью).
- Меню: `GET /admin/menus` (список меню), `GET|POST /admin/menus/{name}` (форма; «Сохранить» или кнопка пункта — поле `action`).
- Файлы: `GET /admin/files` (загрузка и таблица, `?page=N`), `POST /admin/files` (загрузка), `GET /admin/files/{id}`
  (карточка: адрес, замена, где используется), `POST /admin/files/{id}/replace` (замена тем же форматом),
  `GET|POST /admin/files/{id}/delete` (подтверждение, удалить; используемый — 409).
- Медиатека: `GET /admin/media` (загрузка и сетка, `?page=N`), `POST /admin/media` (загрузка), `GET /admin/media/{id}`
  (карточка), `GET|POST /admin/media/{id}/delete` (подтверждение, удалить).
- Зарезервированные slug — `reservedSlugs` в `cmd/site/server.go` (`admin`, `static`, `media`, `files`, `healthz` и раздел
  постов из `posts.BlogPath`): их маршруты перекрывают
  страницу, админка такие slug не принимает. Маршрут-поддерево (`/static/`) занимает и путь без слеша (`GET /static` → 404),
  иначе его забрал бы `GET /{slug}`; e2e `TestE2EReservedSlugsShadowPages` проверяет каждый slug из списка.
- Всё остальное — оформленная 404 (`web.NotFound`).
- `GET /healthz` — JSON о состоянии: `{"status":"ok","version":"<версия сборки>","schema":{"images":{"version":1,"pending":0},"files":{"version":1,"pending":0},"auth":{"version":1,"pending":0},"site":{"version":15,"pending":0}}}`.

## Бинарник

`site serve | migrate | backup | seed | user create EMAIL | reprocess-images -below N`. Миграции применяются только `site migrate`, никогда при старте.

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

## Резервные копии

- `site backup` (`make backup`) пишет в `BACKUP_DIR` пару с одной меткой времени UTC: `<время>.db` — снимок БД
  (`VACUUM INTO`, как перед миграциями) и `<время>.tar.gz` — `uploads/` и `files/` из `DATA_DIR` с этими путями
  в архиве. Права обоих — `0600` (в БД хеши паролей и сессии). Архив появляется только целиком (временный файл
  `.backup-*.tmp` и rename); недописанные файлы хранилища (`*.tmp-*`) в него не попадают.
- Сервер может работать. Сначала снимок, потом архив: хранилище пишет файл до строки в БД и удаляет после, поэтому
  всё, на что ссылается снимок, уже на диске. Пропасть может только файл, удалённый во время архивации (редактор удалил
  изображение, идёт `reprocess-images`), — он перечисляется в логе (`file deleted during backup`).
  Если `DATA_DIR/site.db` нет — ошибка, а не снимок пустой БД.
- На сервере — таймер systemd `deploy/site-backup.timer` (каждую ночь в 03:30) запускает `deploy/site-backup.service`:
  `site backup`, затем `find` удаляет копии старше 14 дней (и снимки перед миграциями). Вручную —
  `systemctl start site-backup`, журнал — `journalctl -u site-backup`. Без systemd — то же строкой cron от пользователя сайта:
  `30 3 * * * site set -a; . /etc/site/site.env; /opt/site/site backup && find "$BACKUP_DIR" -name '*Z.*' -mtime +14 -delete`.
  Копии на том же диске не спасают от потери диска или сервера: их копирование в другое место (rsync, rclone,
  снимки диска у хостера) настраивается для каждого сайта. При хранилище S3 загрузки защищает версионирование бакета.
- Восстановление — в пустой `DATA_DIR` (не поверх: старые `site.db-wal` и `-shm` испортят снимок):
  ```sh
  cd /var/lib/site && systemctl stop site && mv data data.old && install -d -o site -g site -m 0750 data
  tar -xzf backups/<время>.tar.gz -C data && cp backups/<время>.db data/site.db && chown -R site:site data
  systemctl start site-migrate && systemctl start site     # site-migrate — если бинарник новее копии
  ```
  Тест `TestBackupRestore` (`cmd/site/backup_test.go`) проверяет этот путь: сайт из копии отдаёт изображения и файлы.

## Развёртывание

Основной вариант — бинарник под systemd на VPS (`deploy/`); образ Docker — опция.

| Файл | Куда | Что |
|---|---|---|
| `bin/site` (`make build GOOS=linux GOARCH=amd64`) | `/opt/site/site` | бинарник |
| `deploy/site.env.example` | `/etc/site/site.env` (`root:site`, `0640`) | настройки: `ADDR=127.0.0.1:8080`, `BASE_URL`, `DATA_DIR`, `BACKUP_DIR`, `TZ` |
| `deploy/site.service` | `/etc/systemd/system/` | `site serve`; `Restart=on-failure`, ограничения systemd (`systemd-analyze security` — 1.7) |
| `deploy/site-migrate.service` | `/etc/systemd/system/` | `site migrate`, только вручную — не `ExecStartPre`: миграции не применяются при старте |
| `deploy/site-backup.service`, `.timer` | `/etc/systemd/system/` | ночной `site backup` (см. «Резервные копии») |

- Данные — `/var/lib/site` (`StateDirectory`: создаёт systemd, владелец — пользователь `site`, `0750`); `DATA_DIR` и
  `BACKUP_DIR` — внутри. Всё остальное для процесса только чтение (`ProtectSystem=strict`).
- Первая установка:
  ```sh
  useradd -r -s /usr/sbin/nologin site && install -d /opt/site /etc/site
  install -m 0755 site /opt/site/site
  install -o root -g site -m 0640 site.env /etc/site/site.env      # из deploy/site.env.example
  cp site.service site-migrate.service site-backup.service site-backup.timer /etc/systemd/system/
  systemctl daemon-reload && systemctl start site-migrate
  systemctl enable --now site site-backup.timer
  sudo -u site sh -c 'set -a; . /etc/site/site.env; /opt/site/site user create me@example.com'
  ```
- Обновление: новый бинарник рядом, затем остановка, миграции (снимок БД — внутри `site migrate`), замена, старт:
  ```sh
  install -m 0755 site /opt/site/site.new
  systemctl stop site && mv /opt/site/site.new /opt/site/site && systemctl start site-migrate && systemctl start site
  curl -s 127.0.0.1:8080/healthz      # version — новая, pending — 0
  ```
  С непримененными миграциями `serve` не стартует — systemd перезапускает его и пишет причину в `journalctl -u site`.
- Сайт слушает только localhost; TLS и порты 80/443 — у reverse proxy. Caddy сам получает сертификат:
  `example.com { reverse_proxy 127.0.0.1:8080 }`. Без https вход в админку не работает: cookie с флагом `Secure`
  (`COOKIE_SECURE` по умолчанию `true`). Ограничение попыток входа (модуль `auth`) считается по email, поэтому прокси
  ему не мешает; в логе входа `remote_addr` — адрес прокси.
- Несколько сайтов на одном сервере — свои пользователь, юниты, `StateDirectory`, `/etc/<имя>/` и порт `ADDR`.

**Docker (опция).** `make docker` собирает бинарник под Linux (`bin/linux-<arch>/site`) и образ `DOCKER_IMAGE:VERSION`
(`Dockerfile`: distroless `static-debian13:nonroot` с закреплённым digest, пользователь 65532, без shell). Бинарник
строится на машине с доступом к модулям Keelkit — SSH-ключ в сборку образа не попадает; `.dockerignore` пропускает
в контекст только бинарники. Для VPS на amd64 — `make docker DOCKER_ARCH=amd64`. Данные и копии — в томе `/data`:
```sh
docker run --rm -v site-data:/data keelkit-reference migrate
docker run -d --name site -p 127.0.0.1:8080:8080 -v site-data:/data -e BASE_URL=https://example.com keelkit-reference
docker exec site /site backup                            # копии — в /data/backups того же тома
```

**litestream (опция).** Непрерывная репликация БД в S3-совместимое хранилище: WAL уходит каждую секунду, после
потери сервера теряются секунды, а не сутки. Сайт уже в режиме WAL, код не меняется: litestream — отдельный процесс
(свой systemd-юнит из его пакета) рядом с `site.service`. Загрузки (`uploads/`, `files/`) он не копирует — их
по-прежнему защищает `site backup` и копирование архивов на другой сервер. Конфиг `/etc/litestream.yml`:
```yaml
dbs:
  - path: /var/lib/site/data/site.db
    replicas:
      - url: s3://<бакет>/site.db     # ключи — LITESTREAM_ACCESS_KEY_ID / LITESTREAM_SECRET_ACCESS_KEY
```
Восстановление: `litestream restore -o /var/lib/site/data/site.db s3://<бакет>/site.db` в пустой `DATA_DIR`, затем
архив загрузок. Версия — последний стабильный релиз (`v0.5.17` на 2026-10-14), проверки — по разделу «Зависимости»
корневого `AGENTS.md`; решается для каждого сайта.

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
- Своя картинка для телефона (другой кадр той же сцены) — `web.PictureMobile(img, mobile, alt, sizes)`: перед
  основными источниками два `<source media={ web.MobileMedia }>` (WebP и JPEG) со своими `srcset`, `width`, `height`;
  браузер скачивает одну из двух картинок. `web.MobileMedia` — единственная граница «телефон / компьютер» на сайте
  (экран уже 768 px). `mobile == nil` — разметка как у `web.Picture`. Только файл: alt один на обе, пропорции и
  фокус для телефона не задаются; `og:image` и RSS — всегда основная. Где есть: блоки `image`, `image_text`,
  элементы галереи (`image_mobile` в JSON блока, см. «Блоки»), обложка поста (`cover_mobile`). Задана без основной —
  ошибка у поля «Сначала выберите основную картинку»; та же, что основная, — сбрасывается. Мобильные картинки — в
  `<type>_images`, как основные: используемую не удалить из медиатеки.
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
  со ссылками на изображения добавляет туда свой запрос (настройки, страницы, затем посты). Используемое изображение: на карточке — список,
  подтверждение удаления и `POST` — 409 со списком.

## Файлы (модуль `files`)

Как подключать модуль — `modules/files/UPGRADE.md` (v0.1.0); здесь — как это сделано в эталоне. Файлы — документы,
видео и SVG как есть; фотографии для страниц — по-прежнему медиатека (`images`).
- Сервис создаёт `openFiles(db, cfg)` в `cmd/site/main.go`: `files.NewDiskStorage(DATA_DIR/files)` +
  `files.NewService(db, st, filesConfig)`; лимит — `filesConfig` (по умолчанию 50 МБ; сайт с видео задаёт `MaxBytes`).
  Его получают `newMux` и `seed`. Таблицы `files_*` сайт не читает запросами — только через `files.Service`.
- Раздел «Файлы» — `internal/filelib/` (группа «Контент», иконка `files`): загрузка формой `multipart/form-data`
  (htmx и без JavaScript), ошибки модуля (`ErrTooLarge`, `ErrUnsupportedFormat`, `ErrInvalidFile`, `ErrFormatMismatch`) —
  422 у поля; тело — `maxBody(filesAdmin.MaxUploadRequest(), …)` снаружи `signedIn` (лимит модуля + 1 МБ, считается от
  `files.Service.MaxBytes`). Карточка — адрес на сайте, просмотр видео и SVG, форма замены (тот же формат: адрес
  с расширением не меняется), где используется, удаление.
- Ссылка из контента — блок `file` (см. «Блоки»): id файла в JSON блока и строка в `page_files` / `post_files`
  (`file_id TEXT REFERENCES files_files (id)` без `ON DELETE`, миграция `00014`), перезаписываются в той же
  транзакции, что и копия, как `<type>_images`. «Где используется» — `fileUsage` в `cmd/site/server.go`
  (`filelib.UsageFunc`); используемый файл — 409 со списком.
- Адрес — `web.FileURL(f)` (`/files/` + `File.Path`, уже экранирован); формат и размер для людей — `web.FileFormat`,
  `web.FormatBytes`.
- Seed — `cmd/site/seedfiles/` (PDF на одну страницу, SVG-логотип, MP4 на 2 секунды, 4 КБ) через `seedFiles`; id
  дописываются в ту же карту по имени файла, что и изображения; страница «Документы» (`/documents`) — блоки file.

## Шаблоны и HTML

- Шаблоны — templ (`*.templ` рядом с кодом, `*_templ.go` генерируются и коммитятся).
- `internal/web`: `LayoutMeta(web.Meta)` — оболочка публичной страницы с данными для поисковиков (см. «SEO»), `Layout(title)` —
  то же без адреса (ошибки, заглушки); `NotFoundPage`, `ErrorPage`; `Render` (рендер в буфер, затем ответ),
  `NotFound`, `ServerError` (логирует, клиенту — страница 500 без подробностей).
- Данные шапки (`web.Site`: настройки сайта — см. «Настройки сайта», меню — см. «Меню», `BaseURL`, путь ленты) собирает `siteLoader` в `cmd/site/server.go`; middleware `web.WithSite`
  кладёт их в контекст (и путь запроса, и согласие с cookies), layout читает через `web.SiteFrom(ctx)`. HTML-маршруты оборачиваются в него, `/healthz` и `/static/` — нет.
  `internal/web` не импортирует типы контента: связывает их только `cmd/site`.
- `richtext` (Markdown) — модуль `modules/richtext`: `richtext.Render` → goldmark (GFM, сырой HTML не пропускается) → bluemonday (UGC; `class="language-*"`
  у `code`; `nofollow` только у внешних ссылок) → типографика. Результат — тип `richtext.HTML`, в шаблоне выводится как `@body`.
  Типографика: неразрывный пробел (U+00A0) после коротких предлогов, союзов и частиц (`shortWords`, без учёта регистра;
  цепочка «и в лесу» связывается целиком) и перед тире, отбитым пробелами (« — », « – »; диапазон «2020–2025» не трогается).
  Работает по тексту уже очищенного HTML (токенизатор `golang.org/x/net/html`): теги и атрибуты не меняются, текст в
  `pre`/`code`/`kbd`/`samp` — тоже; строчные теги прозрачны («в **лесу**»), блочные разрывают связь. Символы не заменяются
  (кавычки, дефис → тире): только пробелы. `PlainText` (описания, RSS) типографику не делает.
  `templ.Raw` для пользовательского контента не используем.
- Статика — `static/` (пакет `static`, embed). В шаблоне ссылка только через `static.Path("имя")`: она добавляет `?v=<хеш>`,
  а неизвестное имя паникует (ловится e2e). Вендоренные библиотеки и шрифты — с версией в имени файла, без CDN
  (правило — в корневом `AGENTS.md`, «Стек сайта»). Шрифтов в эталоне нет: системный стек в `style.css`.
- Стили — `static/style.css`, чистый CSS с переменными; тёмная тема по `prefers-color-scheme`. Админка дополнительно
  подключает `static/admin.css` (после `style.css`, правила внутри `body.admin`): своя палитра, верхняя панель, боковое
  меню, формы и таблицы — карточками.

## Админка и авторизация

Админка рассчитана на JavaScript (htmx и свой JS в `static/`), запасного варианта без скрипта не делаем; публичный
сайт — без обязательного JavaScript (корневой `AGENTS.md`, «Стек сайта»). Поведение на JavaScript покрывают браузерные
тесты (`make e2e-browser`, входит в `make check`).

Вход, сессии и CSRF — модуль `auth` (как подключать — `modules/auth/UPGRADE.md`, v0.1.0); в `internal/admin/` —
страница входа, оболочка админки и страницы ошибок. «Простой режим»: любой пользователь из `users` — администратор;
`users.role` зарезервирована (по умолчанию `admin`). Таблицы `users`/`sessions` сайт не читает и не пишет запросами —
только через `auth.Service`; их миграция — у модуля (`00003` сайта — та же схема с `IF NOT EXISTS`, для баз до этапа 10.4).

- `admin.NewHandler(db, admin.Options{…})` создаёт `auth.NewService`: `LoginPath` — `/admin/login`, cookie `Secure`
  по `COOKIE_SECURE` (`InsecureCookies`), `Forbidden` — `ForbiddenPage`, `ServerError` — `admin.ServerError`,
  `Now` — `Options.Now` (в тестах — `config.now`).
- Что делает модуль: пароли argon2id (хеш со старыми параметрами пересчитывается при входе), политика — от 8 символов
  до 1024 байт (`auth.CheckPasswordPolicy`), email в нижнем регистре; сессия — cookie `session` на 14 дней, в БД —
  SHA-256 токена, при входе всегда новый токен; cookie `HttpOnly`, `SameSite=Lax`; 5 неудач за 15 минут для email —
  блокировка до конца окна (счётчики в памяти процесса); для неизвестного email пароль сверяется с фиктивным хешем.
- Маршруты админки оборачиваются в `newMux`: `adminOnly` (`http.CrossOriginProtection` + данные шапки), защищённые — ещё и
  `signedIn` (`adminH.RequireUser`: `RequireUser` модуля и данные меню оболочки в контексте): без сессии — 303 на
  `/admin/login?next=…` (для GET), небезопасный метод без CSRF-токена сессии — 403. Ответы админки — `Cache-Control: no-store`, страницы — `noindex`.
- Вход (`admin.Handler.Login`): `auth.Service.Login`, ошибки — в форме: `ErrLoginCSRF` 403 «Форма устарела»,
  `*auth.LockedError` 429 (минуты из `RetryAfter`), `ErrBadCredentials` 422 — одно сообщение для неверного email и пароля.
  `next` после входа — только путь внутри `/admin` (`auth.SafeNext`), иначе дашборд.
- CSRF-токен сессии: в формах — скрытое поле `auth.CSRFField` (`admin.CSRFInput()`), в htmx — заголовок `auth.CSRFHeader`
  (layout админки ставит `hx-headers` на `<body>`). Форма входа — double submit cookie (`auth.Service.LoginCSRF`).
- Пакет не знает о типах контента: счётчики дашборда передаёт `adminCounters` в `cmd/site/server.go` через `admin.Options.Counters`
  (`Counter.URL` — ссылка на раздел), пункты меню — `admin.Options.Sections` (группа «Контент»). Новый тип контента добавляет туда
  счётчик и раздел. Разделы всего сайта (настройки, меню) — `admin.Options.SiteSections`, группа «Сайт» ниже.
- Для разделов типов контента пакет экспортирует: `admin.Layout(title)` (оболочка: верхняя панель и боковое меню `.site-nav`,
  текущий пункт — по префиксу пути; иконка пункта — `Section.Icon`: `pages`, `posts`, `tags`, `people`, `image`, `files`, `settings`, `menu`, иначе папка),
  `admin.CSRFInput()` (скрытое поле токена), `admin.NotFound`, `admin.ServerError`, `admin.Redirect` (303 или
  `HX-Redirect` после успешного POST), `admin.IsHTMX`, `admin.Live(url, formID)` (форма слева, живой предпросмотр справа —
  см. «Живой предпросмотр»).
- htmx в админке: `<meta name="htmx-config">` разрешает подмену разметки ответом 422 (по умолчанию htmx не подменяет 4xx).
- Пользователи: `site user create` (`make user-create`) и seed — `auth.Service.CreateUser`. Dev-пользователь из seed:
  `admin@example.com` / `adminadmin` (seed в проде не запускается).
- Не получается войти: проверить dev-пользователем (вход работает?); затем раскладку при `make user-create` и не остался ли
  запущенным сервер со старой БД после `make reset` (порт 9090 занят — `make dev` после seed падает с «address already in use»).
  После 5 неудач email заблокирован на 15 минут; перезапуск сервера снимает блокировку.

## Тип контента

Один тип — один пакет `internal/content/<type>/` (образцы — `pages`; `posts` — то же с обложкой, категорией и датой
публикации; `categories` — справочник без черновика):
`queries.sql` + код sqlc, `handlers.go` (`Handler`, `NewHandler(db, images, files…)`, публичные хендлеры), `views.templ`,
`admin.go` (`Admin`, `NewAdmin(db, media, mediaURL, files, filesURL, …)`, `AdminPath`, `EditPath`, хендлеры раздела админки,
`SeedPublished` для seed), `admin.templ`. Поле `blocks` — через `internal/blocks/` (см. «Блоки»).
Миграция — в `db/migrations/`, маршруты — в `newMux`, dev-данные — в `cmd/site/seed.go`, запись sqlc — в `sqlc.yaml`,
счётчик дашборда — в `adminCounters`, пункт меню — в `admin.Options.Sections`, feature `sitemap`/`rss` — см. «SEO».
Seed блога (`seedBlog`): 3 категории, 13 опубликованных постов (`post-01`…`post-13`, даты — дни назад от запуска,
обложки у каждого третьего; у самого нового `post-13` — лид, ручной анонс и alt обложки), черновик `draft-post`
и `future-post` с датой через 30 дней. Seed людей (`seedPeople`): трое по порядку, фото у первого (`photo.jpg`);
страница «Команда» (`/team`) — текст и блок `people` (в меню её нет); «Документы» (`/documents`) — блоки `file`.

Раздел админки (образец — `pages/admin.go`):
- Маршруты — `signedIn(...)`: сессия и CSRF уже проверены. Список, `new`, создание `POST /admin/<type>`,
  форма и сохранение `/{id}`, предпросмотр черновика `/{id}/view`, снятие с публикации `POST /{id}/unpublish`,
  удаление через страницу подтверждения `/{id}/delete` (работает без JavaScript); у типа с полем `blocks` — живой
  предпросмотр `POST /admin/<type>/live` (см. «Живой предпросмотр»).
- Кнопки формы (черновик-копия — см. ниже): «Сохранить» (черновик), «Опубликовать», «Отмена» (ссылка на список, без
  сохранения). «Снять с публикации» и удаление — не рядом с ними, а отдельным блоком `.danger-zone` под чертой;
  в строках списка ссылки «Удалить» нет. Удалить можно только неопубликованную запись (запрос — `AND NOT EXISTS` опубликованной копии);
  подтверждение и `POST` для опубликованной отдают 409 с объяснением. Главная (`home`) всегда опубликована:
  не снимается с публикации (409), не удаляется, опубликованная главная не меняет slug (ошибка у поля).
- Порядок полей формы записи — от содержания к служебному (образец — `posts`): заголовок, лид, обложка, блоки;
  затем раздел «Публикация» (`fieldset.form-section`: дата, категория, анонс); затем свёрнутые разделы
  (`details.form-section`) «Адрес» (slug с неизменяемым началом `.input-prefix`, старые адреса; в `summary` —
  текущий адрес, раскрыт при ошибке в его полях) и «Поисковики и соцсети»; в конце кнопки. Пояснение кнопок —
  в их `title`, не отдельным текстом под ними.
- Форма отправляется и обычным POST, и через htmx (`hx-post`, `hx-target="this"`, `hx-swap="outerHTML"`).
  Ошибки валидации — 422: при `HX-Request` только фрагмент формы, без него — страница целиком; введённые данные
  возвращаются в форму, сообщение — у поля (`#<поле>-error`, `aria-invalid`, `aria-describedby`).
- Успех: обычной форме — 303 на форму с `?saved=1` или `?published=1`, htmx — 204 и `HX-Redirect`.
- Нормализация в `validate`: `TrimSpace` заголовка, CRLF → LF в тексте. Уникальность — ограничением `UNIQUE` в БД
  (`database.IsUniqueViolation`), без предварительного `SELECT`: так не проходит и гонка двух запросов.
- slug — `internal/slug`: пустой составляется из заголовка (`slug.Make`, транслитерация кириллицы своей таблицей),
  введённый вручную приводится к тому же виду; затем проверка `reservedSlugs` и уникальности.
- Поле `image` (обложка поста): `select` из медиатеки (последние `libraryLimit` и выбранное) и превью; превью слева,
  выбор и поля этого изображения (alt, подпись) — справа одной колонкой (`.block-image-pick` > `.block-thumb` +
  `.block-image-fields`; так же у блоков `image`, `image_text` и элементов галереи); смена выбора
  через htmx перерисовывает только поле (`POST …/cover`, `hx-params` — только поля обложки). Выбранного изображения нет
  в медиатеке — ошибка у поля. Своя обложка для телефона (`cover_mobile`, поле `mobile: true`) — второй выбор
  в том же поле (см. «Своя картинка для телефона» в «Живой предпросмотр»).
- Поля простого текста (`text`: лид и анонс поста) — textarea без превью, `TrimSpace` и CRLF → LF, лимит длины;
  на сайте выводятся как текст, без Markdown. alt обложки (`cover_alt`) — в поле обложки, рядом с выбором картинки:
  htmx-перерисовка поля отправляет и его (`hx-params="cover,cover_mobile,cover_alt"`); без обложки alt не сохраняется,
  пустой — обложка декоративная (`alt=""`).
- Анонс поста (`posts.summary`): ручной `excerpt` целиком, в одну строку; пусто — `blocks.Summary` начала текста.
  Один источник для ленты, RSS и `meta description` (с этапа SEO записи — и для подстановки в пустое SEO-описание).
- Поле `ref` (категория поста): `select` с вариантом «— без категории —»; варианты — запрос в пакете типа
  (`ListCategoryOptions`), а не импорт пакета другого типа. Несуществующий id — ошибка у поля, не число — 400.
- Тип без своего адреса с ручным порядком (образец — `people`, feature `order` с `spec v0.5.0`): записи правят в своём
  разделе, на сайт их выводит блок-список (см. «Блоки») на любой странице. Колонка `sort_order` (`INTEGER NOT NULL`,
  индекс `(sort_order, id)`), новая запись — в конец (`max + 1`). Кнопки ↑ ↓ в строках списка — submit с `action`
  (`up:<id>`, `down:<id>`) формы вокруг таблицы: сразу меняют порядок на сайте, без «Сохранить»; сервер переписывает
  `sort_order` всех записей по порядку одной транзакцией (плотно, без повторов). У первой строки ↑ и у последней ↓
  выключены. Типы с feature `order` — `orderTypes` (`cmd/site/spec_test.go`).
- Справочник без черновика (образец — `categories`): форма «Сохранить» и «Отмена», правки сразу на сайте;
  удаление через подтверждение, используемую запись — 409 со списком (запрос и ошибка внешнего ключа).
  Публичную страницу справочника отдаёт тип, который на него ссылается (лента категории — пакет `posts`),
  поэтому адреса для админки категорий передаёт `newMux` (`categories.NewAdmin(db, posts.CategoryPath, posts.EditPath)`).
- Поле `richtext` (и Markdown в блоках): textarea + превью (`hx-post` на `…/preview?field=<имя>`, `hx-params` — только это поле,
  `hx-trigger="input changed delay:400ms"`, `hx-sync="this:replace"`); превью при открытии формы рисует сервер,
  ответ превью — тот же `richtext.Render`.
  Над textarea — панель оформления (`markdownToolbar` в `internal/blocks/form.templ`, `static/editor.js`): жирный,
  курсив, зачёркнутый, ссылка, подзаголовок `##`, списки, цитата, код (надписи — классические английские: B, I, S, Link,
  H2, UL, OL, Quote, `</>`; подсказка `title` — по-русски, `aria-label` начинается с надписи). Кнопка оборачивает выделение разметкой Markdown
  (пробелы по краям — снаружи), повторное нажатие снимает её; строчные — у каждой строки выделения; Ctrl/⌘+B, I, K
  (по `ev.code` — работает в любой раскладке). Вставка — `execCommand('insertText')`: правка отменяется Ctrl+Z, событие
  `input` обновляет превью. Подчёркивания нет: в Markdown его нет, `<u>` не пропускается. Панель — надстройка: без
  JavaScript её не видно (CSS показывает `.md-toolbar` только при классе `js` на `<html>`, его ставит скрипт), кнопки —
  `type="button"`. Метка поля — `<label for>`, не обёртка: кнопки внутри `label` перехватили бы её связь с textarea.
  Обработчики делегированы на `document`, поэтому блоки, добавленные через htmx, работают сразу.

### Черновик-копия (образец — `pages`)

Заменяет флажок `draft` (решение в корневом `AGENTS.md`): правки опубликованной записи не видны на сайте до «Опубликовать».
- Таблица `<type>s` — только идентичность (`id`, `created_at`, `updated_at`); содержимое — `<type>_contents(<type>_id,
  kind IN ('draft','published'), поля…)`, `PRIMARY KEY (<type>_id, kind)`. Черновик есть всегда, опубликованная копия —
  если запись на сайте. slug — часть копии: уникален частичными индексами `WHERE kind = 'draft'` и `WHERE kind = 'published'`,
  адрес на сайте меняется при публикации.
- «Сохранить» (`action=save`, и Enter в поле — первая кнопка формы скрытая «Сохранить») пишет черновик и его ссылки на
  изображения одной транзакцией; «Опубликовать» (`publish`) — то же и копирует черновик в опубликованную (`INSERT … SELECT …
  ON CONFLICT DO UPDATE`) вместе со ссылками; «Снять с публикации» — `DELETE` опубликованной копии (ссылки — каскадом).
- Публичные запросы (страница, меню, sitemap и RSS) берут только `kind = 'published'`; админка — черновик.
- Редиректы со старых адресов — `<type>_redirects(slug PRIMARY KEY, <type>_id … ON DELETE CASCADE)` (миграция `00007`):
  перед «Опубликовать» и «Снять с публикации» прежний опубликованный slug записывается туда (`RememberPublishedSlug`,
  в той же транзакции), после публикации запись с новым slug удаляется (`DropRedirectToPublished`). Публичный `Show`
  при промахе ищет slug в редиректах (`RedirectTarget`) и отвечает 301 на текущий адрес, иначе 404. Опубликованная
  копия всегда важнее редиректа: slug, занятый другой записью, открывает её.
- Старые адреса вручную (образец — посты, `posts/oldpaths.go`): поле «Старые адреса» в форме — по пути в строке после
  раздела (`old_slug`, `2019/05/post.html`), для переноса с другой CMS, где адреса не проходят правила slug. Форма
  показывает все строки `<type>_redirects` записи (и запомненные прежние slug): убрать строку — убрать редирект.
  Сохраняются при «Сохранить» и «Опубликовать» (до копирования: прежний slug добавится к ним). Нормализация: пустые
  строки и повторы убираются, `%`-кодирование раскрывается, `/` по краям отбрасываются (их же отбрасывает `Show`).
  Ошибка у поля: домен, `?`/`#`, пустой путь, `category/<x>` (занят лентой категории), адрес опубликованного поста,
  старый адрес другого поста. Маршрут поста — `{slug...}`, чтобы под редирект попадали и пути с `/`.
  Seed: у `post-01` — старый адрес `seedOldPath`.
- Статус в списке и в форме: «Черновик» (нет опубликованной), «Опубликована», «Есть неопубликованные изменения»
  (черновик отличается от опубликованной: сравниваются колонки, блоки — строкой JSON, `blocks.Marshal` детерминирован).
- Предпросмотр `/{id}/view` — черновик в публичном шаблоне с плашкой `.draft-banner`; «Открыть на сайте» — по
  опубликованному slug. Рядом с формой — живой предпросмотр несохранённой формы (см. «Живой предпросмотр»).
- Feature `publish_date` (образец — `posts`): дата — часть копии. Поле `datetime-local` в часовом поясе сервера;
  пустое — «в момент публикации»: «Опубликовать» ставит текущее время (до минуты, как в поле) в черновик и копию.
  Опубликованная копия с датой в будущем на сайте не видна до этой даты; статус — «Запланирован на …», ссылки
  «Открыть на сайте» нет. Лента — по дате публикации, новые сверху.

### Живой предпросмотр (`admin.Live`, `static/live-preview.js`)

Страница редактирования записи с блоками (образцы — `pages`, `posts`) делится на две части: слева форма, справа запись
в шаблоне сайта, обновляется по мере ввода. Строится из **несохранённой формы**: автосохранения нет, «Сохранить»
остаётся явным действием, модель черновика-копии не меняется.
- Шаблон: `FormPage` оборачивает содержимое в `@admin.Live(AdminPath+"/live", "<id формы>") { … }`; хлебные крошки
  и шапка (`admin-title`: заголовок, статус, «Предпросмотр черновика», «Открыть на сайте») — до него, над обеими
  панелями. Панель, граница
  и вкладки рендерятся с `hidden` и включаются скриптом: без JavaScript — одна форма, как раньше, и ссылка
  «Предпросмотр черновика» (`/{id}/view`).
- Хендлер `Live` — `POST /admin/<type>/live`: `parseForm` → `viewData` (то же, что у превью сниппета) → `View(v)`
  с `Preview` (noindex, без canonical) и `Live` (без плашки черновика). Ничего не пишет и не проверяет: показывает и
  недописанную форму (неверная дата поста — без даты). Поля, которых нет в `viewData` для сниппета (у поста — лид,
  дата, категория), хендлер дозаполняет сам. id не нужен — работает и для новой записи.
- Скрипт: ввод и `change` в форме — запрос через 500 мс (`fetch`, urlencoded, CSRF — полем формы; новый запрос
  отменяет старый); после htmx-подмены формы или блока (`htmx:afterSettle`) — тоже. Первый ответ — `srcdoc` iframe,
  следующие заменяют только `<main>`: прокрутка не сбрасывается. Кнопка «Обновить» загружает страницу заново.
- Ширина устройства — кнопки «Компьютер» 1440 и «Телефон» 390 px (`liveDevices`; планшета нет: у сайта две ширины,
  граница — `web.MobileMedia`): iframe рисуется в этой ширине и масштабируется под панель (`transform: scale`),
  подпись — ширина и масштаб. Граница панелей перетаскивается (и стрелками с клавиатуры);
  ширина устройства и формы запоминаются в `localStorage`. Уже 64rem — вкладки «Редактор / Предпросмотр».
- Своя картинка для телефона: кнопки ширины переключают и режим формы — `data-image-mode="desktop|mobile"` на корне
  `.live`. Поле картинки (`blocks.imageField`, обложка поста `coverField`) содержит обе пары «превью + выбор»
  в обёртках `.image-variant[data-image-for="desktop|mobile"]` (`display: contents`); CSS показывает пару режима,
  вторая скрыта, но остаётся в форме — сохранение от режима не зависит. Ошибка у поля телефона видна в любом режиме
  (`:has(.field-error)`). В режиме телефона без своей картинки — приглушённое превью основной `.image-inherited`
  «как на компьютере», пустой вариант выбора — «— как на компьютере —». Метка «Своя для телефона»
  (`.status-mobile`) — в обоих режимах. На узком экране тот же переключатель — над формой, рядом с вкладками
  (`liveWidths` дважды; кнопки обеих групп переключаются вместе). Без скрипта атрибута нет — режим «Компьютер».
- Связь блоков — по якорю `block-<id>`: он одинаков у секции формы блока и у блока на сайте, iframe того же
  происхождения. Щелчок по блоку в предпросмотре прокручивает к форме блока и подсвечивает её (`.live-flash`); щелчок
  по заголовку блока в форме прокручивает предпросмотр к блоку. Ссылки в предпросмотре не открываются, баннер
  cookies скрипт убирает (он не часть записи).
- Пока предпросмотр рядом с формой, превью Markdown у каждого поля скрыто (CSS); во вкладках на узком экране — остаётся.
- Новый тип с полем `blocks`: хендлер `Live`, поле `Live` во `viewData` и условие плашки в `View`, обёртка
  `admin.Live` в `FormPage`, маршрут `POST /admin/<type>/live`, e2e по образцу `cmd/site/e2e_live_test.go`.

### Блоки (`internal/blocks/`)

Поле `{ type: blocks, allow: [...] }` из `site.yaml` — JSON-массив в колонке (`CHECK (json_valid(…) AND json_type(…) = 'array')`).
Общие поля блока: `id` (стабильный: якорь `block-<id>` на сайте, имена полей формы `block-<id>-<поле>`), `type`, `hidden`
(остаётся в данных и в админке, на сайт не выводится). Типы: `text` (Markdown), `image` (изображение, alt обязателен,
подпись), `image_text` (изображение, alt, подпись, Markdown, `position` left/right), `quote` (цитата, автор),
`gallery` (список изображений с alt и подписью; на сайте — лента с прокруткой на CSS scroll-snap, без JavaScript;
у `image`, `image_text` и элемента галереи — необязательная `image_mobile`, своя картинка для телефона: в JSON только
если задана, см. «Изображения»),
`file` (файл из раздела «Файлы» и подпись: PDF — ссылка с форматом и размером, текст — подпись или имя файла; видео —
`<video controls preload="metadata">`, подпись — под ним; SVG — `<img>`, подпись — его alt, поэтому обязательна),
`people` (блок-список: все люди в порядке из их раздела).
- Файлы блоков `file`: `blocks.ResolveFiles(ctx, src, list)` → `blocks.Files` (как `Resolve` для изображений);
  `blocks.Validate(list, res, files)`, `blocks.View(list, res, files, lists)`; `blocks.FileIDs` — для `<type>_files`.
  Форма — `FormEnv.Files` (выбранные), `FileLib` (последние загруженные, `blocks.FileLibrary` — `files.Service`),
  `FilesURL` (ссылка на раздел). Блок со ссылками на файлы реализует `fileIDs()` (необязательный интерфейс).
- Блок-список (`blocks.Listing`; страница с вступлением и карточками записей — «витрина»): выводит все записи типа
  контента, сам полей не хранит — только место среди блоков. Записи и их вывод даёт код типа: `blocks.ListSource`
  (`Render` — компонент со всеми записями, `AdminURL` — раздел для ссылки в форме блока); источники по имени блока —
  `blockLists` в `cmd/site/server.go`, их получают `pages.NewHandler` и `pages.NewAdmin` (предпросмотр).
  `ListSources.Resolve` загружает записи только видимых блоков, вывод — `blocks.View(list, res, files, lists)`; блок без
  источника не выводится (предупреждение в лог). `TestBlockListsMatchKinds` сверяет `blockLists` с `blocks.ListKinds`.
  Новый блок-список: тип контента (обычно с feature `order`), запись в `blocks.Kinds` с `newListing`, метод вывода в
  пакете типа (`people.Handler.Cards`), запись в `blockLists`, имя — в `allow`. Свои поля блоку-списку (заголовок,
  фильтр по категории) добавлять, когда они понадобятся сайту. Страница целиком своей вёрстки (шаблон страницы) —
  тоже по потребности сайта, не раньше.
- Чтение (`blocks.Unmarshal`): нет поля — нулевое значение (смысл по умолчанию задаёт тип); нет или повторный id — новый;
  неизвестный тип — `*blocks.Unknown`: на сайт не выводится (предупреждение в лог), в админке сохраняется как есть.
  Ломающее изменение полей блока — миграция данных по строгим правилам.
- Форма: блоки по порядку (`block` — id, затем `block-<id>-type`, `block-<id>-hidden`, поля типа). Кнопки блока —
  submit с `name="action"`: `up:<id>`, `down:<id>`, `hide:<id>`, `remove:<id>`, `add:<тип>` (`blocks.Apply`); сервер
  возвращает форму изменённой (200, плашка «Изменения ещё не сохранены»), ничего не сохраняя. Без JavaScript — обычный
  POST всей формы, с htmx — подмена формы. «Добавить блок» — меню на `<details class="block-add">` (без JavaScript
  раскрывается так же, после выбора `static/editor.js` его закрывает); кнопка типа через htmx запрашивает только фрагмент блока
  (`hx-post …/block?type=`, `hx-params="none"`, `hx-swap="beforeend"`); смена изображения перерисовывает блок (`?id=`).
- Списки элементов внутри блока (`items.go`, образец — `gallery`): у элемента стабильный id (как у блока; нет или
  повторный в старых данных — новый), порядок — поля `block-<id>-item`, поля элемента — `block-<id>-i-<элемент>-<поле>`
  (`blocks.ItemField`). Кнопки — те же submit с `action`: `item-up:<блок>:<элемент>`, `item-down:…`, `item-remove:…`,
  `item-add:<блок>` (не больше `blocks.MaxItems`). Ошибка — у поля элемента (`#block-<id>-i-<элемент>-<поле>-error`);
  испорченные id элементов в форме — 400. Тип со списком реализует `itemList` (`itemIDs`, `fixItemIDs`, `applyItem`)
  через общие с полем-списком записи `listfield.Apply` и `listfield.FixIDs` (см. «Поле-список»); картинки элементов —
  в `imageIDs`, поэтому они попадают в `<type>_images`.
- Проверка — `blocks.Validate` (все блоки, и скрытые); ошибки — у поля блока (`#block-<id>-<поле>-error`), 422 как у
  обычных полей. Изображения загружает `blocks.Resolve` (интерфейс `blocks.Images` — `Get` модуля).
- `blocks.ImageIDs` — для `<type>_images` (скрытые блоки тоже держат изображение; и картинки для телефона —
  необязательный интерфейс `mobileRefs`), `blocks.PlainText` и `blocks.Summary` —
  текст для описания страницы (`web.Meta`), RSS, позже поиска; `blocks.FirstImage` — картинка страницы для `og:image` (только основные, `imageIDs`).
- Список блоков поля — в коде типа (`pages.BodyBlocks`, `posts.BodyBlocks`); `TestSpecBlocksMatchCode` сверяет каждое
  поле `blocks` из `site.yaml` со списком своего типа (новое поле `blocks` — запись в карте `code` теста).

Как добавить тип блока: запись в `blocks.Kinds`; структура с JSON-тегами и методами `Content` в `types.go`
(`readForm`, `check` — нормализация и ошибки, `view`, `form`, `imageIDs`, `plainText`; со списком элементов — ещё
методы `itemList`, см. `Gallery`); templ-вывод в `view.templ`
(корень — элемент с классом `block block-<тип>` и `id="block-<id>"`) и фрагмент формы в `form.templ`; стили; тест в
`blocks_test.go`; имя — в `allow` нужного поля `site.yaml` и в списке блоков типа контента.

### Поле-список (`internal/listfield/`)

Поле записи `{ type: list, fields: {…} }` из `site.yaml` (не блока; образец — `social` в настройках): элементы по
порядку, поля элемента задаёт спецификация. Хранение — JSON-массив в колонке `TEXT NOT NULL DEFAULT '[]'
CHECK (json_valid(…) AND json_type(…) = 'array')` (`listfield.Encode`, `listfield.Decode` — элементу без id или с
повторным даёт новый). Картинки элементов (поле `image`) — ещё и в `<type>_images`, как у блоков.
- В пакете типа — структура элемента с полем `ID` (JSON-теги — имена полей из `site.yaml`, `id` служебный), чтение
  формы, проверка и вывод. Общее — `internal/listfield`: id (`NewID`, `ValidIDs`), действия (`Apply`), имена формы,
  кнопки (`listfield.Controls`, `listfield.Add`).
- Форма поля `<field>`: id элементов по порядку — скрытые `<field>-item` (`listfield.Order`; испорченные или повторные —
  400, `listfield.IDs`), поле элемента — `<field>-i-<элемент>-<поле>` (`listfield.Name`; это же ключ ошибки,
  `#<имя>-error`). Кнопки — submit с `action`: `list-up:<field>:<элемент>`, `list-down:…`, `list-remove:…`,
  `list-add:<field>` (`listfield.ActionValue`, `ParseAction`; не больше `listfield.MaxItems`). Сервер возвращает форму
  изменённой (200, плашка «Изменения ещё не сохранены»), ничего не сохраняя; без JavaScript — обычный POST, с htmx —
  подмена формы. Первая кнопка формы — скрытая «Сохранить» (Enter в поле). Чужое поле, элемент или действие — 400.
- Новый элемент — пустой в конец; элемент, где все поля пустые, при сохранении отбрасывается молча, у заполненного
  частично — ошибки у полей.

### Адрес раздела постов

Адрес раздела — константа `posts.BlogPath` (один сегмент пути: `/blog`, `/news`); спецификация его не задаёт. Из неё
строятся маршруты (`newMux`), адреса постов и категорий (`posts.PublicPath`, `posts.CategoryPath`), `reservedSlugs`,
пункт-раздел меню, sitemap, RSS и подсказки админки; тесты — через `blogPath` (`e2e_blog_test.go`). Поэтому сменить
адрес — правка одной константы; `/blog` руками в коде и тестах не пишут (с `/news` проходят `make test` и
`make e2e-browser`). Если сайт уже работал под старым адресом раздела, для старых ссылок нужен отдельный маршрут
с 301 (`GET /blog/{path...}` → тот же путь под новым разделом) — в эталоне его нет. Название раздела («Блог» в
ленте, хлебных крошках, меню) — текст шаблонов и `menuSections`.

## Настройки сайта (`internal/settings/`)

Поля — `settings.fields` в `site.yaml` (с `spec v0.3.0`); одна запись `site_settings` (`id = 1`), колонки — имена полей
(`TestSpecSettingsMatchCode` сверяет их с таблицей после миграций). Черновика нет: «Сохранить» — и сразу на сайте.
Строки нет (прод до первого сохранения) — `settings.Get` отдаёт только `settings.DefaultName`; сохранение создаёт строку
(`INSERT … ON CONFLICT DO UPDATE`).
- Поля эталона: название (обязательно), описание, картинка для соцсетей по умолчанию (`share_image`), адрес (в несколько
  строк), телефон, email, текст и ссылка баннера cookies, коды Яндекс.Вебмастера и Google Search Console, ссылки на
  соцсети (`social`, поле-список: название до 50 символов и адрес `http(s)://` с хостом; миграция `00011`). Пустое — не
  выводится. Колонки идут в порядке полей `site.yaml`: новое поле — в конец. Другой сайт меняет набор полей: колонка в миграции, поле в `site.yaml`, форма, `web.Site`, вывод в layout.
- Раздел админки — одна форма (`/admin/settings`, группа «Сайт» в меню), группы полей — `fieldset.settings-group`;
  ошибки — 422 у поля, как у типов контента (с htmx — только форма). Нормализация: однострочные поля — пробелы в один,
  адрес — CRLF → LF, без пустых строк. Телефон — с цифрами; email — один адрес без имени (идёт в `mailto:`); ссылка
  cookies — путь на сайте (`/privacy`, не `//host`) или `http(s)://` с хостом; код подтверждения — `[A-Za-z0-9_-]`,
  вставленный целиком мета-тег сводится к `content`.
- `share_image` — как обложка поста: `select` из медиатеки и превью, смена перерисовывает поле (`POST …/image`); ссылка
  без `ON DELETE`, медиатека показывает «Настройки» среди использующих (`settings.UseImage` в `imageUsage`).
- На сайте (`siteLoader` → `web.Site`, `internal/web/layout.templ`): мета-теги `yandex-verification` и
  `google-site-verification` на каждой странице (файлы в корне не нужны); `og:image` страницы без своей картинки —
  картинка из настроек, без `og:image:alt` (`web.Defaults`; им же пользуется превью сниппета в админке); контакты —
  `<address>` в подвале, телефон — ссылка `tel:` из цифр (`web.TelURL`); соцсети — `ul.social-links` в подвале
  (`web.Site.Social`, `rel="me noopener"`).
- Баннер cookies (`web.CookieNotice`): внизу страницы (`position: sticky`), пока нет cookie `cookie_notice=1`. «Понятно» —
  форма `POST /cookie-notice` со скрытым `back` (путь текущей страницы): без JavaScript — 303 обратно (только путь на этом
  сайте, иначе `/`), с htmx — пустой ответ заменяет баннер. Cookie на год, `HttpOnly`, `SameSite=Lax`, `Secure` по
  `COOKIE_SECURE`; маршрут — под `http.CrossOriginProtection`. Это уведомление, а не выбор категорий cookie: сторонних
  счётчиков в эталоне нет.

## Меню (`internal/menu/`)

Меню — `menus: [main, footer]` в `site.yaml` (с `spec v0.3.0`; только имена, устройство пунктов — код сайта) и
`siteMenus` в `cmd/site/server.go` (имя, название и подсказка для админки; `TestSpecMenusMatchCode` сверяет имена).
Пункты всех меню — таблица `menu_items` (миграция `00012`), черновика нет: «Сохранить» — и сразу на сайте.
- Пункт ведёт ровно на одно (`CHECK`): страницу `page_id` (адрес — из текущего опубликованного slug, поэтому смена slug
  меню не ломает), свой адрес `url` (путь на сайте или `http(s)://` с хостом, название обязательно) или раздел `section`
  (ключ из `menuSections`: `blog` — лента блога). Пустое название — заголовок страницы или раздела. У раздела с
  `Section.Children` флажок `children` — подпункты из его записей (у блога — категории, где сейчас есть видимые посты,
  `posts.CategoryLinks`); они идут после подпунктов, заданных вручную.
- Вложенность — один уровень: `parent_id` — пункт верхнего уровня того же меню (`ON DELETE CASCADE`), `position` — порядок
  среди соседей. Сохранение заменяет все пункты меню одной транзакцией (id строк меняются, ссылок на них нет).
- Ссылка на страницу — без `ON DELETE`: страницу из меню не удалить. Раздел страниц получает `pageUsage`
  (`pages.UsageFunc`) и отдаёт подтверждение удаления и `POST` с 409 и ссылками на меню (`UsedPage`); снятие с
  публикации разрешено — пункт на неопубликованную страницу на сайте не выводится (вместе с подпунктами), в админке
  помечен «Страница не опубликована», в выборе — «(не опубликована)». Раздел, которого больше нет в коде, так же скрыт.
- На сайте (`menu.Site` в `siteLoader` → `web.Site.Menus[имя]`, дерево `web.Link` с `Children`): `web.WithSite` отмечает
  `Current` (точный путь, `aria-current="page"`) и `Active` (путь под адресом пункта или текущий подпункт, класс `active`).
  Layout эталона: `main` — `nav.site-nav` в шапке (подпункты — `ul.sub`, выпадающий по `:hover` и `:focus-within`),
  `footer` — `nav.footer-nav` в подвале (подпункты колонкой). Пустое меню не выводится. Как выводить меню — дизайн сайта.
- Раздел админки — группа «Сайт»: список меню с числом пунктов и форма меню. Пункты — плоский список полей `listfield`
  (`items-item`, `items-i-<пункт>-target|title|url|children|child`): `target` — `page:<id>`, `section:<ключ>` или `url`
  (`menu.PageTarget`, `SectionTarget`, `URLTarget`), `child=1` — подпункт ближайшего пункта выше. Кнопки — submit с
  `action`: `list-up|down|indent|outdent|remove:items:<пункт>`, `list-add:items` (`menu.apply`): пункт верхнего уровня
  переносится и убирается вместе с подпунктами, подпункт — только среди своих; «Вложить» — у пункта без подпунктов.
  Форма возвращается изменённой без сохранения, как у поля-списка. Поле адреса и флажок подпунктов CSS показывает
  только при подходящем выборе в «Куда ведёт» (`:has`; без него поля видны). Пустой пункт отбрасывается; ошибки — у поля
  пункта, 422.
- Набор меню у каждого сайта свой: меню — это место в вёрстке (шапка, подвал, боковая колонка), его задаёт разработчик,
  редактор только наполняет. Сайту без меню в подвале — `menus: [main]`, с тремя — `menus: [main, footer, sidebar]`.
  Добавить меню: имя в `menus` в `site.yaml`, запись в `siteMenus` и вывод `site.Menus["<имя>"]` в шаблоне (константа
  рядом с `mainMenu` в `layout.templ`); миграция не нужна — `menu_items.menu` без ограничения на имена. Убрать: то же
  в обратную сторону; оставшиеся строки меню в БД просто не выводятся (удалить их можно миграцией).
  Меню, которые редактор создаёт сам (и назначает на места или вставляет блоком на страницу), отложены до
  потребности реального сайта (принцип 6).
- Seed (`seedMenus`, только пустые меню): в шапке «О проекте», «Контакты», блог с категориями; в подвале «О проекте»
  с подпунктом «Контакты» и свой адрес «RSS». Другой раздел (лента нового типа) — запись в `menuSections`.

## SEO (`internal/seo/`, `web.Meta`)

Пишется в коде эталона; вынос в модуль — после второго сайта. Пакет `seo` не знает о типах контента.
- `<head>` — `web.LayoutMeta(web.Meta{…})`: `Title`, `Description` (пусто — описание сайта; у типов — `blocks.Summary`),
  `Path` — адрес страницы для `canonical` и `og:url` (у ленты — с `?page=N`; лишние параметры запроса не попадают),
  `Article` и `Published` (пост: `og:type article`, `article:published_time`), `Image` (`og:image` — JPEG не шире
  1600 px, `web.ShareImage`; у поста — обложка, иначе первое изображение блоков, иначе картинка из настроек), `ImageAlt` (`og:image:alt`; у поста —
  alt обложки), `NoIndex`. Описание поста — анонс (ручной или начало текста). Без `Path` — нет canonical
  и Open Graph (404, 500). Предпросмотр черновика — без `Path` и с `NoIndex`. Абсолютные адреса — `web.Site.BaseURL` +
  путь; на каждой странице — `<link rel="alternate">` на RSS.
- SEO записи вручную — общий компонент `internal/seofields` у каждого типа со своим адресом (страницы, в том числе
  главная, и посты; у справочников без адреса его нет). В `site.yaml` — feature `seo` (с `spec v0.3.0`; поля
  `seo_title`, `seo_description`, `seo_image` она добавляет сама, в `fields` их не пишут); `TestSpecFeaturesMatchCode`
  сверяет типы с feature `seo` со списком `seoTypes`.
  Колонки — в копии записи (`<type>_contents`, миграция `00009`): правка SEO опубликованной записи видна после
  «Опубликовать» и отмечает «Есть неопубликованные изменения»; `seo_image` — в `<type>_images`, как обложка.
  Пустое поле — подстановка, её задаёт тип в `viewMeta` (у поста — заголовок, анонс, обложка; у страницы — заголовок,
  начало текста, первое изображение блоков), затем `Fields.Apply` накладывает заданное: SEO-заголовок идёт в `<title>`
  и `og:title` как есть, без названия сайта (`web.Meta.ExactTitle`); описание — в `meta description` и `og:description`
  (анонс в ленте и RSS не меняется); картинка — в `og:image`. Пробелы и переводы строк — один пробел.
  Форма: `seofields.Section(f.SEO, f.SEOEnv)` — свёрнутый `<details>` (раскрыт, если что-то задано или ошибка) с превью
  выдачи и карточки соцсетей (`seofields.Snippet` — то же, что выведет `web.LayoutMeta`, `<title>` — `web.TitleTag`);
  превью обновляется через htmx при вводе в любое поле формы (`hx-trigger="input delay:500ms from:closest form"`,
  `hx-include="closest form"`, `POST /admin/<type>/seo`: хендлер разбирает форму как при сохранении и собирает
  `viewData` из неё), без JavaScript — после отправки. Лимиты 200/500 символов, превью предупреждает о длине
  сверх ~60/160. Новый тип со своим адресом: колонки, feature `seo` в `site.yaml`, `SEO` в форме, `Apply` в `viewMeta`,
  маршрут `…/seo`, запись в `seoTypes` (`cmd/site/spec_test.go`).
- `/sitemap.xml` — `seo.Sitemap(baseURL, sources…)`: адреса от типов с feature `sitemap` в `site.yaml` —
  `sitemapSources` в `cmd/site/server.go` (метод `SitemapURLs` пакета типа: только видимое на сайте). Страницы — без
  `lastmod` (у копии нет времени публикации), лента блога и посты — `lastmod` по дате публикации. Один файл: лимит
  протокола — 50 000 адресов.
- `/rss.xml` — `seo.Feed(baseURL, feedSource)`: RSS 2.0, последние 20 видимых постов (`posts.FeedItems`), анонс —
  ручной или `blocks.Summary` (500 символов, без разметки), категория, обложка — `<enclosure>` JPEG; `guid` — `post-<id>`
  (`isPermaLink="false"`), не меняется со сменой slug. Типы с feature `rss` — `feedTypes`.
- `/robots.txt` — `seo.Robots(baseURL, "/admin/")` и ссылка на sitemap.
- Новый тип с feature `sitemap` или `rss`: метод `SitemapURLs` / `FeedItems` в пакете типа, запись в `sitemapSources`
  или `feedTypes` (несколько типов в ленте — слить записи по дате в `feedSource`); `TestSpecFeaturesMatchCode` сверяет
  списки с `site.yaml`. Публичные шаблоны типа передают `web.Meta` с `Path`.

## Тесты

- Unit-тесты — рядом с пакетом (`internal/database`, `internal/slug`, `internal/blocks`, `internal/menu`,
  `internal/listfield`).
- e2e — `cmd/site/e2e_test.go`, функции `TestE2E*`: `httptest.NewServer(s.handler(config{}))` (все маршруты `newMux`) над БД после `migrate` и `seed`,
  HTML разбирается goquery. Хелперы `fetch` (тело уже прочитано и закрыто) и `page` (статус, `Content-Type`, документ).
- e2e админки — `cmd/site/e2e_admin_test.go`: `adminClient` (cookie jar, редиректы не выполняются), `post`, `login`, `loginOK`
  (вход dev-пользователем, возвращает CSRF-токен сессии), `csrfToken` (скрытое поле формы).
- `TestImagesContract` (`cmd/site/images_test.go`) — контрактные тесты модуля (`testkit.Run`) на интеграции сайта:
  `migratedSite` (миграции модуля и сайта, загрузки во временной папке) и маршруты `newMux`. При обновлении модуля
  они первыми покажут, что интеграция сломалась.
- `TestFilesContract` (`cmd/site/files_test.go`) — контрактные тесты модуля `files` на интеграции сайта (хранилище
  в `DATA_DIR/files`, маршруты `newMux`).
- `TestAuthContract` (`cmd/site/auth_test.go`) — контрактные тесты модуля `auth`: настоящие страницы входа и админки
  из `newMux` с cookie `Secure`, время сайта (`config.now`) задаёт testkit — проверяются вход, выход, CSRF, блокировка,
  истечение сессии. `TestMigrationAuthOverSiteTables` (`migrations_test.go`) — база до этапа 10.4 (таблицы создала
  миграция сайта) мигрирует, пользователь входит.
- `TestRichtextContract` (`cmd/site/richtext_test.go`) — контрактные тесты модуля `richtext`: Markdown публикуется текстовым
  блоком страницы (`pages.SeedPublished`), testkit проверяет HTML, который отдаёт сервер (XSS-примеры обезврежены).
- Хелпер `migratedSite(t, withSeed)` (`main_test.go`) — БД после `migrate`, сервисы изображений и файлов и `handler(cfg)`;
  `withSeed` заливает `seedContent` без изображений (их обработка ~1 с). Нужны картинки из seed — `migratedSiteWithImages(t)`
  (полный seed, возвращает id изображений по имени файла).
- e2e файлов — `cmd/site/e2e_files_admin_test.go`: страница «Документы» (ссылка, картинка, плеер; тип, Range, CSP у SVG),
  раздел «Файлы» (загрузка формой и htmx, ошибки, карточка, замена по тому же адресу, 409 для используемого, удаление),
  блок file в форме страницы (выбор, ошибка «файла нет», ссылка в «где используется»). Хелпер `newFilesAdmin` —
  seed без изображений, но с файлами.
- e2e медиатеки — `cmd/site/e2e_media_admin_test.go`: seed, загрузка (htmx и форма), каждая ошибка проверки файла,
  тело сверх лимита, карточка, удаление, 409 для используемого изображения, постраничный вывод.
- e2e раздела типа контента — `cmd/site/e2e_<type>_admin_test.go` (образец — `pages`): `newPageAdmin` (сайт + вошедший клиент),
  `form` (поля формы так, как их отправит браузер — `formValues`; другая форма — `formByID`), `submit(path, form, action, header)`, `addBlock`,
  заголовки `htmx`; каждая ошибка валидации проверяется и без `HX-Request` (страница), и с ним (фрагмент).
- e2e блога — `cmd/site/e2e_blog_test.go` (лента, страницы, категория, пост, скрытые черновик и будущий пост) и
  `cmd/site/e2e_posts_admin_test.go` (посты и категории в админке; хелперы `createPost`, `postForm`, `newPostForm`).
- e2e своей картинки для телефона — `TestE2EPageMobileImages` (блоки `image`, `image_text`, галерея) и
  `TestE2EAdminPostCoverMobile` (обложка): сохранение, ошибки у поля, публикация копирует ссылки, `<source media>`
  на сайте, `og:image` — основная, 409 медиатеки, убрать — прежняя разметка.
- `TestSpecBlocksMatchCode` (`cmd/site/spec_test.go`) — блоки из `allow` в `site.yaml` есть в коде и совпадают со списком типа;
  `TestSpecFeaturesMatchCode` — типы с feature `sitemap`, `rss`, `seo` и `order` совпадают с `sitemapSources`, `feedTypes`, `seoTypes`
  и `orderTypes` (у таблиц `orderTypes` есть `sort_order`); `TestBlockListsMatchKinds` — `blockLists` совпадают с блоками-списками;
  `TestSpecSettingsMatchCode` — поля `settings` совпадают с колонками `site_settings`; `TestSpecMenusMatchCode` — `menus`
  совпадают с `siteMenus`; `TestSpecModulesMatchCode` — `modules` (с `spec v0.4.0`) совпадают с модулями Keelkit
  в `go.mod` (мажорные версии тоже), а модули с миграциями стоят в порядке `migrationSources`.
- e2e настроек — `cmd/site/e2e_settings_test.go`: сохранение и вывод на сайте (подвал, мета-теги, баннер), каждая ошибка
  поля (форма и htmx), соцсети (кнопки списка формой и htmx, порядок после сохранения, ошибки у элемента, 400), картинка по умолчанию в `og:image` и в превью сниппета, 409 медиатеки, согласие с cookies
  (форма, htmx, `back` только на этот сайт, запрос с чужого сайта — 403).
- e2e меню — `cmd/site/e2e_menus_test.go`: список меню, кнопки пунктов (формой и htmx), сохранение и вывод в шапке и
  подвале (`menuLinks`), смена slug страницы, подпункты вручную и из записей, неопубликованная страница, каждая ошибка
  поля, 400; 409 удаления страницы из меню — в `TestE2EAdminPageUnpublishDelete`.
- e2e людей и блока people — `cmd/site/e2e_people_admin_test.go`: порядок кнопками (формой и htmx, 400/404), вывод на
  странице «Команда», создание и ошибки полей, удаление, скрытый блок и пустой список, фото (htmx-поле, 409 медиатеки).
- e2e живого предпросмотра — `cmd/site/e2e_live_test.go`: разметка панели в форме, страница из несохранённой
  формы (новая цитата, лид, дата, категория), ничего не сохранено, новая запись без id, 403 без CSRF.
- e2e SEO записи — `cmd/site/e2e_seo_admin_test.go`: превью сниппета (форма и htmx), `<title>`, описание и `og:image`
  поста и страницы (и главной), очистка — снова подстановка, ошибки полей, `seo_image` держит изображение.
- e2e SEO — `cmd/site/e2e_seo_test.go`: sitemap и RSS разбираются `encoding/xml` (без черновиков, будущих постов и
  неопубликованных страниц, адреса от `BASE_URL`), robots.txt, canonical и Open Graph, `noindex` предпросмотра,
  редиректы со старых slug. В тестах `BASE_URL` — `testBaseURL` (`https://example.test`; `handler(config{})` подставляет его сам).
- Браузерные тесты — `cmd/site/browser_test.go` (`//go:build browser`, `TestBrowser*`, chromedp, headless Chrome):
  поведение админки на htmx и своём JS — превью при наборе, сохранение через `HX-Redirect`, подмена формы с ошибкой
  без перезагрузки, «Добавить блок», «↑», «Скрыть» и выбор изображения у блока, выбор обложки поста, превью сниппета SEO, загрузка файла (`hx-encoding="multipart/form-data"`),
  выбор картинки и «Добавить ссылку» в настройках, «Понятно» в баннере cookies (`TestBrowserSettingsAndCookieBanner`),
  «Добавить пункт» меню, поле адреса по выбору и подпункты в шапке по фокусу (`TestBrowserMenu`),
  панель оформления Markdown (`TestBrowserMarkdownToolbar`: кнопка, повторное нажатие, Ctrl+B, отмена, ссылка, список),
  ↓ в списке людей (`TestBrowserPeopleOrder`), живой предпросмотр (`TestBrowserLivePreview`: ввод и «Добавить блок» видны
  в iframe, «Телефон», щелчок по блоку в предпросмотре, ссылка в нём не открывается, вкладки на узком экране),
  своя картинка для телефона (`TestBrowserMobileImage`: режим полей по кнопкам ширины, выбор своей, `currentSrc`
  в предпросмотре «Телефон» и «Компьютер», сохранение в режиме «Компьютер», переключатель над формой на узком экране).
  После каждого htmx-действия — `htmxSettled()`: обработчики на новый фрагмент htmx навешивает после фазы settle,
  и клик chromedp раньше этого ушёл бы обычной отправкой формы. Запуск — `make e2e-browser` (входит в `make check`); `make test` их не собирает, линтер — собирает (`build-tags` в `.golangci.yml`).

## Спецификация

`site.yaml` — источник правды о структуре сайта: структурные изменения сначала вносятся в него, затем в код.
Проверка (из корня монорепо): `make spec-validate FILE=reference/site.yaml`.

## Структура

```
cmd/site/main.go            # точка входа: конфиг, подкоманды, источники миграций
cmd/site/server.go          # маршруты, данные шапки (siteLoader), /healthz
cmd/site/seed.go            # dev-данные: seedContent, seedImages
cmd/site/seeddata/          # изображения для seed (embed)
cmd/site/seedfiles/         # файлы для seed: PDF, SVG, MP4 (embed)
cmd/site/user.go            # site user create
cmd/site/backup.go          # site backup: снимок БД и архив загрузок
cmd/site/e2e_test.go        # e2e (TestE2E*)
cmd/site/e2e_admin_test.go  # e2e админки
cmd/site/e2e_pages_admin_test.go  # e2e раздела «Страницы»
cmd/site/e2e_media_admin_test.go  # e2e медиатеки
cmd/site/e2e_files_admin_test.go  # e2e файлов: раздел «Файлы», блок file, отдача
cmd/site/e2e_blog_test.go         # e2e блога на сайте
cmd/site/e2e_posts_admin_test.go  # e2e разделов «Посты» и «Категории»
cmd/site/e2e_seo_test.go          # e2e sitemap, RSS, robots.txt, <head>, редиректов
cmd/site/e2e_live_test.go         # e2e живого предпросмотра в редакторе записи
cmd/site/e2e_seo_admin_test.go    # e2e SEO записи (internal/seofields) в админке и на сайте
cmd/site/e2e_settings_test.go     # e2e настроек сайта: раздел админки, подвал, мета-теги, баннер cookies
cmd/site/e2e_menus_test.go        # e2e меню: раздел админки, шапка и подвал
cmd/site/e2e_people_admin_test.go # e2e людей: порядок, раздел админки, блок people на странице
cmd/site/migrations_test.go # миграции, меняющие данные, на копии реалистичных данных
cmd/site/schema_test.go     # db/schema.sql ↔ миграции
cmd/site/spec_test.go       # site.yaml ↔ код (модули, блоки, features, поля настроек, меню)
cmd/site/images_test.go     # контрактные тесты модуля images (testkit)
cmd/site/files_test.go      # контрактные тесты модуля files (testkit)
cmd/site/richtext_test.go   # контрактные тесты модуля richtext (testkit)
cmd/site/auth_test.go       # контрактные тесты модуля auth (testkit)
cmd/site/browser_test.go    # браузерные тесты (тег browser, make e2e-browser)
db/migrations/              # миграции сайта (goose) + embed.go
db/schema.sql               # итоговая схема БД после всех миграций (make schema, TestSchemaSnapshot)
internal/admin/             # админка: страница входа и оболочка (модуль auth), меню, дашборд, страницы ошибок, Live (редактор с предпросмотром)
internal/blocks/            # поля blocks: типы блоков, JSON, форма, вывод на сайте
internal/listfield/         # поля list: id элементов, действия ↑ ↓ «Убрать» «Добавить», имена формы, JSON
internal/content/pages/     # тип контента pages: queries.sql, sqlc, handlers.go, views.templ, admin.go, admin.templ
internal/content/posts/     # посты: то же + dates.go (дата публикации); лента категории — тоже здесь
internal/content/categories/ # категории: queries.sql, sqlc, admin.go, admin.templ (без публичных шаблонов)
internal/content/people/    # люди (без адреса, ручной порядок): admin.go, admin.templ, handlers.go (Cards), views.templ
internal/database/          # открытие SQLite, статус схем, снимок, применение миграций
internal/media/             # медиатека в админке (модуль images)
internal/filelib/           # раздел «Файлы» в админке (модуль files)
internal/menu/              # меню (menu_items): запросы, сборка для сайта (Site), раздел админки
internal/seo/               # sitemap.xml, rss.xml, robots.txt
internal/seofields/         # SEO записи вручную: поля, проверка, раздел формы с превью сниппета, наложение на web.Meta
internal/slug/              # slug: Make (транслитерация), Valid
internal/settings/          # настройки сайта (site_settings): запросы, Get, раздел админки (admin.go, admin.templ)
internal/web/               # layout (Layout, LayoutMeta, Meta, Site), баннер cookies, страницы ошибок, рендер, Picture, PictureMobile
static/                     # CSS (style.css, admin.css), htmx, editor.js (панель Markdown), live-preview.js (живой предпросмотр), иконка + static.go (embed, Path, Handler)
deploy/                     # юниты systemd (serve, migrate, backup + таймер), пример /etc/site/site.env
Dockerfile, .dockerignore   # образ (опция) из бинарника make build
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: новые `internal/content/<type>/`.
