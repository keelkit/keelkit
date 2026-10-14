# Связи и общий код

Где тип контента, модуль, настройки и меню связаны с остальным кодом сайта. Компилятор найдёт не всё: списки,
которые сверяют тесты `cmd/site/spec_test.go`, и тексты (названия, язык) — только по этому чек-листу.

## Тип контента: все места

Новый тип — добавить в каждое подходящее место; тип эталона, которого нет в спецификации, — убрать из каждого.

| Место | Что | Когда |
|---|---|---|
| `internal/content/<тип>/` | пакет типа | всегда |
| `db/migrations/NNNNN_<тип>.sql` | таблицы | всегда |
| `sqlc.yaml` | запись пакета | всегда |
| `cmd/site/server.go`, `newMux` | хендлеры, `NewAdmin`, маршруты сайта и админки | всегда |
| `admin.Options.Sections` | пункт меню админки (группа «Контент») | всегда |
| `adminCounters` | счётчик дашборда | всегда |
| `imageUsage` | «где используется» изображение | есть `image`, `blocks` или `list` с `image` |
| `fileUsage` | «где используется» файл | `blocks` с блоком `file` |
| `pageUsage` / аналог | что ссылается на запись (409 удаления) | на тип ссылаются `ref`, элементы списков или меню |
| `reservedSlugs` | первый сегмент раздела | у типа есть лента `/<раздел>` |
| `menuSections` | пункт-раздел меню (с подпунктами из записей, если нужно) | у типа есть лента |
| `sitemapSources` | `SitemapURLs` | feature `sitemap` |
| `feedTypes`, `feedSource` | лента RSS | feature `rss` |
| `seoTypes` (`spec_test.go`) | SEO записи | feature `seo` |
| `orderTypes` (`spec_test.go`) | ручной порядок | feature `order` |
| `blockLists`, `blocks.Kinds` | блок-список типа | тип выводится блоком на страницах |
| карта `code` в `TestSpecBlocksMatchCode` | список блоков поля | поле `blocks` |
| `cmd/site/seed.go` | dev-данные, порядок вызовов (сначала то, на что ссылаются) | всегда |
| `cmd/site/e2e_*_test.go`, `browser_test.go` | тесты | всегда |
| `AGENTS.md` | маршруты, тип контента, seed, тесты, структура | всегда |

Признак, что тип эталона убран полностью: `git grep -i -E '<тип>|<Тип>'` находит только слова в обычном тексте.

## Модули

- Нет `files`: удалить `internal/filelib/`, `internal/web/files.go`, `openFiles` и `filesConfig` (`main.go`), источник
  миграций `files`, маршруты `/files`, `/admin/files`, `fileUsage`, раздел «Файлы», блок `file` (из `blocks.Kinds`,
  `types.go`, `view.templ`, `form.templ`, `FormEnv.Files`, `ResolveFiles`, тестов блоков), `<тип>_files`,
  `seedFiles` и `cmd/site/seedfiles/`, `files_test.go`, `e2e_files_admin_test.go`, файлы из `backup` (`filesDir`
  и проверка в `backup_test.go`), `files` из `site.yaml`; `go mod tidy`. Раздел «Файлы» `AGENTS.md` — убрать.
- `images`, `richtext`, `auth` — обязательны (на них стоят медиатека, блоки, админка).
- Порядок `migrationSources` (`cmd/site/main.go`) — как `modules` в `site.yaml`, сайт — последним.

## Настройки

Колонки `site_settings` — поля `settings.fields` по порядку, имена — из спецификации (`TestSpecSettingsMatchCode`).
Поле по смыслу совпадает с полем эталона, но называется иначе (`socials.network` ≈ `social.name`,
`cookie_banner` ≈ `cookie_text`) — бери код эталона и имя из спецификации. Поле без аналога — в форму настроек,
в `web.Site` и в layout там, где оно по смыслу нужно (контакт — в подвал); неясно где — спросить.

Нет поля эталона — его функции на сайте нет: убрать из `settings`, формы, `web.Site`, layout и тестов (нет
`cookie_text` — нет баннера cookies и `POST /cookie-notice`; нет `yandex_verification` — нет мета-тега).
Нет `name` или `description` — константы `settings.DefaultName` / `settings.DefaultDescription` из ответов
человека (или из `site.name`), которые `siteLoader` и `feedSource` отдают вместо полей.

**Сайт без настроек или меню** (человек отказался): нет `internal/settings/` — `siteLoader` собирает `web.Site` из
констант (название, описание), раздела «Настройки» и его тестов нет, `share_image` — нет (у `og:image` — только своя
картинка записи). Нет `internal/menu/` — шапка выводит разделы сайта постоянным списком в `layout.templ` (главная,
ленты типов), `menu_items`, `siteMenus`, `menuSections`, `pageUsage` по меню и раздел «Меню» не нужны.

## Меню

`menus` из спецификации — `siteMenus` (имя, название, подсказка) и вывод в `layout.templ`; только `main` — убрать
подвал-меню из layout и seed. Пункты seed (`seedMenus`) — страницы и разделы этого сайта.

## Название, язык, имена эталона

- `git grep -n -i -E 'эталон|keelkit-reference|Keelkit reference'` — каждое место переписать под сайт: комментарий
  пакета `cmd/site/main.go`, `settings.DefaultName`, `DOCKER_IMAGE` в `Makefile`, заголовок `static/style.css`,
  seed, `site.yaml`. Упоминания Keelkit как движка (модули, `AGENTS.md`) остаются.
- Язык `site.languages[0]`: `<html lang>` (`internal/web/layout.templ`), `Language` RSS-канала (`feedSource`), все
  тексты публичных шаблонов (лента, «Читать далее», 404, 500, баннер cookies, даты — `humanDate`, номера страниц
  ленты, alt по умолчанию), seed. Админка остаётся на русском на любом сайте. Типографика `richtext` рассчитана на
  русский текст и на другом языке почти ничего не меняет — оставить.
- Стили `static/style.css` — эталонные; дизайн сайта (цвета, шрифты, сетка) — отдельная работа после create-site,
  если человек не дал макет сразу. Шрифты и скрипты — только вендоренными файлами (корневой `AGENTS.md`, «Стек сайта»).
