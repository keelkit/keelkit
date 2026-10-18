# Маршруты и структура

Часть `AGENTS.md` сайта: читается, когда задача касается этой темы.

## Маршруты

- `GET /` — страница со slug `home`; пока её нет — заглушка с названием и описанием сайта.
- `GET /{slug}` — опубликованная копия страницы из `pages` (`page_contents`, `kind = 'published'`); `/home` → 301 на `/`.
  Прежний slug страницы (`page_redirects`) — 301 на её текущий адрес; у постов так же (`post_redirects`), если пост виден.
- Блог — под `posts.BlogPath` (в эталоне `/blog`, см. «Адрес раздела постов» в `docs/content-types.md`): `GET /blog` (лента, `?page=N`, по 10
  постов), `GET /blog/{slug...}` (пост; иной путь — старый адрес поста, 301), `GET /blog/category/{slug}` (лента
  категории, `?page=N`). Только опубликованные копии с наступившей датой публикации; несуществующая или пустая
  страница ленты (кроме первой), неверный `?page` — 404. В меню блог попадает пунктом-разделом (см. «Меню» в `docs/admin.md`).
- `POST /cookie-notice` — согласие с баннером cookies (`web.AcceptCookies`, см. «Настройки сайта» в `docs/admin.md`); `GET /cookie-notice` —
  обычный `GET /{slug}`, поэтому в `reservedSlugs` его нет.
- `GET /sitemap.xml`, `GET /rss.xml`, `GET /robots.txt` — пакет `internal/seo` (см. «SEO» в `docs/seo.md`); без данных шапки.
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

## Структура

```
cmd/site/main.go            # точка входа: конфиг, подкоманды, источники миграций
cmd/site/server.go          # маршруты, данные шапки (siteLoader), /healthz
cmd/site/seed.go            # dev-данные: seedContent, seedImages
cmd/site/seeddata/          # изображения для seed (embed)
cmd/site/seedfiles/         # файлы для seed: PDF, SVG, MP4 (embed)
cmd/site/user.go            # site user create, site user password
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
docs/                       # конвенции сайта по темам; AGENTS.md — оглавление с картой этих файлов
site.yaml, sqlc.yaml        # спецификация сайта, конфиг sqlc
```

Структура будет расти по плану: новые `internal/content/<type>/`.
