# Тип контента по спецификации

Как превратить запись `content.<тип>` из `site.yaml` в пакет `internal/content/<тип>/`, миграцию и тесты.
Правила хранения, формы, ошибок и htmx — в разделе «БД и миграции» `AGENTS.md` сайта, в `docs/content-types.md`
(«Тип контента», «Блоки», «Поле-список») и `docs/seo.md`; здесь — выбор образца и то, чего в эталоне нет.

## 1. Образец

Выбирается по двум признакам: есть ли черновик (feature `draft`) и есть ли свой адрес (поле `slug`).

| Тип в спецификации | Образец | Хранение | На сайте |
|---|---|---|---|
| `pages` | `pages` | см. ниже: `draft` — черновик-копия, без него — одна таблица | `/` (slug `home`) и `/{slug}` |
| `draft`, `slug`, `publish_date` | `posts` | черновик-копия | лента `/<раздел>` (по 10, `?page=N`) и `/<раздел>/{slug...}` |
| `draft`, `slug`, без `publish_date` | `pages` + адреса `posts` | черновик-копия | лента `/<раздел>` (новые сверху) и `/<раздел>/{slug...}` |
| `slug`, без `draft` | форма `categories`, публичное — `posts` | одна таблица, правки сразу на сайте | как строкой выше |
| `slug`, на него ссылается `ref` типа со своими адресами | `categories` | одна таблица | лента ссылающегося типа, отфильтрованная по записи (`/<раздел>/category/{slug}`) — отдаёт пакет ссылающегося типа |
| без `slug` | `people` (с `order`) или `categories` (без) | одна таблица | своих адресов нет: блок-список на страницах (`people`), или только справочник для `ref` |

- `pages` без `draft` (как в `spec/examples/blog.yaml`): одна таблица `pages` (id, поля, `created_at`,
  `updated_at`), «Сохранить» — сразу на сайте, статусов, «Опубликовать», «Снять с публикации» и предпросмотра
  черновика нет; остальное (главная, slug, `reservedSlugs`, редиректы `page_redirects` при смене slug, 409 удаления
  страницы из меню, главную не удалить) — как в эталоне. Меню читает таблицу страниц напрямую — см. `wiring.md`.
- Раздел ленты — константа `<Тип>Path` пакета (как `posts.BlogPath`): один сегмент, по умолчанию `/<тип>`
  (`/news`, `/services`); название раздела — по смыслу на языке сайта. Константа входит в `reservedSlugs`, пункт-раздел
  меню (`menuSections`) и тесты — так же, как `BlogPath` (раздел «Адрес раздела постов» `docs/content-types.md`).
- Порядок в ленте: `publish_date` — по дате, новые сверху; `order` — `sort_order`; иначе — по `created_at`, новые сверху.
- Тип без `draft`, но с полями `image`, `blocks` или `list` с картинками: ссылки на изображения — прямо колонкой
  (`people.photo` — `TEXT REFERENCES images_files (id)` с индексом), а изображения из JSON (блоки, элементы списка) —
  таблица `<тип>_images(<тип>_id … ON DELETE CASCADE, image_id … REFERENCES images_files (id))`, перезаписывается в
  той же транзакции, что и запись. Так же `<тип>_files` для блока `file`.
- Имя пакета и таблицы — имя типа из спецификации (`news`, `team`, `services`): `internal/content/news/`, таблица
  `news`; копия черновика — `news_contents`, ссылки — `news_images` (в эталоне единственное число — `page_contents`,
  `post_images`; для нового типа бери имя типа как есть, не изобретай единственное число). Разделы админки —
  `/admin/<тип>`.

## 2. Поля

Колонка, Go, форма и вывод. «Обязательное» — `required: true`. Ошибки — у поля, 422, тексты на русском (админка
по-русски на любом сайте). Длина однострочных полей — до 200 символов, текста — до 5000, если смысл поля не требует иного.

| Тип поля | Колонка | Форма | На сайте | Образец |
|---|---|---|---|---|
| `string` | `TEXT NOT NULL DEFAULT ''`; обязательное — `CHECK (x <> '')` | `<input>`, `TrimSpace` | текст | `posts.title`, `people.role` |
| `text` | так же | `<textarea>` без превью, `TrimSpace`, CRLF → LF | текст, абзацы — по пустой строке или `white-space: pre-line` | `posts.lead` |
| `richtext` | так же | `blocks.MarkdownField("<поле>", …)`: textarea `#<поле>`, панель оформления, превью (`POST …/preview?field=`, ответ — `blocks.Preview`) | `richtext.Render`, вывод `@body`; анонс — `blocks.Cut(richtext.PlainText(…), n)` | Markdown блока `text` (`internal/blocks`), «Поле richtext» в разделе «Тип контента» `docs/content-types.md` |
| `int` | обязательное — `INTEGER NOT NULL`; необязательное — `INTEGER` (NULL; `sql.NullInt64`: 0 — значение, а не пусто) | `<input type="number" inputmode="numeric">`, `strconv.ParseInt`; ошибка «Введите целое число» | число; цена — с разделителем разрядов на языке сайта | нет |
| `bool` | `INTEGER NOT NULL DEFAULT 0 CHECK (x IN (0, 1))` (`required` не имеет смысла) | `<input type="checkbox" value="1">` | условие вывода или пометка | `menu_items.children` |
| `date` | `TEXT NOT NULL DEFAULT ''`, формат `2006-01-02`; обязательное — `CHECK (x <> '')` | `<input type="date">`, `time.Parse("2006-01-02")`; ошибка «Неверная дата» | дата на языке сайта (`humanDate` из `posts/dates.go` — по образцу, без времени) | `posts.publish_date` — дата со временем, другое |
| `image` | `TEXT REFERENCES images_files (id)`, NULL — нет (обязательное — `NOT NULL`); с `mobile: true` — ещё `<поле>_mobile` | `select` из медиатеки + превью, смена — htmx-перерисовка поля (`POST …/<поле>`); с `mobile` — живой предпросмотр (ниже) | `web.Picture` / `web.PictureMobile` | `posts.cover`, `people.photo` |
| `ref` | `INTEGER REFERENCES <to> (id)`, NULL — нет; колонка `<поле>_id`, индекс | `select` с «— не выбрано —»; варианты — запрос в своём пакете | ссылка или название | `posts.category` |
| `slug` | `TEXT NOT NULL UNIQUE CHECK (slug <> '')` (у копии — частичные уникальные индексы) | `internal/slug`, «Адрес» в свёрнутом разделе | адрес записи | `pages.slug` |
| `blocks` | `TEXT NOT NULL DEFAULT '[]' CHECK (json_valid… AND json_type… = 'array')`, колонка `blocks` | `internal/blocks`; живой предпросмотр `admin.Live` | `blocks.View` | `pages.body` |
| `list` | `TEXT NOT NULL DEFAULT '[]' CHECK (…)`, колонка — имя поля | `internal/listfield` | по смыслу | `settings.social` |

- `blocks`: список разрешённых блоков — `allow`; набор в коде типа (`<Тип>.BodyBlocks`), запись в карте
  `TestSpecBlocksMatchCode`. Блок из `allow`, которого нет в `blocks.Kinds`, — новый тип блока (раздел «Как добавить
  тип блока» `docs/content-types.md`); блок-список по типу контента — раздел «Блоки», «Блок-список». Блоки эталона, которых нет ни в
  одном `allow`, не удаляй из `internal/blocks` — они не мешают и пригодятся `add-section`.
- `image` с `mobile: true`: картинку для телефона выбирают только в режиме «Телефон» живого предпросмотра
  (`admin.Live`, `data-image-mode`; раздел «Живой предпросмотр» `docs/content-types.md`) — без него второй выбор поля скрыт.
  Поэтому у такого типа обязателен живой предпросмотр, даже без поля `blocks`: хендлер `Live`, маршрут
  `POST /admin/<тип>/live`, обёртка `admin.Live` в форме, e2e по `e2e_live_test.go` (образец — `posts`, обложка).
- `list`: структура элемента в пакете типа с полем `ID`; поля элемента — по той же таблице (без `slug`, `blocks`,
  `list`). `ref` в элементе — `select` как у поля `ref`, но в JSON хранится id: внешнего ключа нет, поэтому
  несуществующий id при выводе пропускается, а при сохранении — ошибка у поля элемента; если на запись ссылаются
  элементы списков, удаление этой записи показывает их в «где используется» (запрос по `json_each`). Картинки элементов —
  в `<тип>_images`.
- Порядок полей формы — от содержания к служебному (раздел «Раздел админки» `docs/content-types.md`).

## 3. Features

| Feature | Что делает | Образец |
|---|---|---|
| `draft` | черновик-копия: `<тип>` + `<тип>_contents(kind)`, «Сохранить» / «Опубликовать» / «Снять с публикации», статус, предпросмотр, редиректы `<тип>_redirects` | `pages`, `posts`; раздел «Черновик-копия» `docs/content-types.md` |
| `publish_date` | колонка `publish_date` (UTC, `2006-01-02T15:04:05Z`), запись видна с этого момента; с `draft` — часть копии, пустая ставится при «Опубликовать»; без `draft` — поле формы, пустое — время сохранения | `posts` |
| `sitemap` | метод `SitemapURLs` (только видимое), запись в `sitemapSources` | `pages`, `posts` |
| `rss` | метод `FeedItems`, запись в `feedTypes`; несколько типов — слить по дате в `feedSource` | `posts` |
| `seo` | колонки `seo_title`, `seo_description`, `seo_image` (у копии — в `<тип>_contents`), `seofields.Section` в форме, `Apply` в `viewMeta`, `POST …/seo`, запись в `seoTypes` | `pages`, `posts` |
| `order` | `sort_order INTEGER NOT NULL` + индекс `(sort_order, id)`, кнопки ↑ ↓ в списке, `POST …/order`, запись в `orderTypes` | `people` |

Публичная страница записи без `seo` всё равно передаёт `web.Meta` с `Path`, описанием и картинкой по подстановке
(как `viewMeta` без `Fields.Apply`).

## 4. Админка

- Раздел — `admin.Section{Title, URL, Icon}` в группе «Контент», счётчик дашборда. Название — по-русски, во
  множественном числе («Новости», «Услуги», «Команда»). Иконки: `pages`, `posts`, `tags`, `people`, `image`, `files`;
  другое имя — папка (новая иконка — в `internal/admin`, по образцу существующих SVG).
- Список: колонки — заголовок (первое `string`-поле), статус у `draft`, дата у `publish_date`; ↑ ↓ у `order`.
- Удаление используемой записи (`ref`, элемент списка, пункт меню) — 409 со списком, как у категорий.

## 5. Seed и тесты

- Seed типа — функция `seed<Тип>` в `cmd/site/seed.go`: 3–5 записей (у ленты — больше 10, чтобы была вторая страница),
  поля заполнены, картинки — из `seeddata/`, ссылки `ref` — на записи seed. Идемпотентно (пропуск по slug или имени).
- e2e типа — `cmd/site/e2e_<тип>_admin_test.go` и публичные проверки в `e2e_test.go` (или своём файле, как
  `e2e_blog_test.go`): каждая ошибка валидации каждого поля (без `HX-Request` и с ним), сохранение и вывод на сайте
  каждого поля, черновик и публикация, дата в будущем, удаление и 409, sitemap/RSS/SEO по features, порядок ↑ ↓.
- Поле без образца в эталоне (`int`, `bool`, `date`, `richtext` у типа, `ref` в элементе списка) — отдельный тест на
  разбор формы, ошибку и вывод.
